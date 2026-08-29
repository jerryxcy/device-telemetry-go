//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jerryxcy/device-telemetry-go/internal/telemetry"
)

// 這些測試需要一個真的 PostgreSQL。TestMain 會用 testcontainers 起一個,
// 資料庫與連線字串見 main_integration_test.go。
//
// 之所以不能用假的 Repository:這裡要驗的正是「pgx 與 PostgreSQL 實際怎麼互動」——
// RETURNING 有沒有真的區分出重複、批次失敗時前面的寫入會不會被回滾。
// 假物件只會照我們的想像回答。

// seedDevice 建一台設備並在測試結束時連同讀數一起清掉。
func seedDevice(t *testing.T, pool *pgxpool.Pool, serial string) {
	t.Helper()
	ctx := context.Background()
	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM readings WHERE serial = $1`, serial)
		pool.Exec(ctx, `DELETE FROM devices WHERE serial = $1`, serial)
	}
	cleanup()
	if _, err := pool.Exec(ctx,
		`INSERT INTO devices (serial, name) VALUES ($1, $2)`, serial, "整合測試"); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	t.Cleanup(cleanup)
}

func countReadings(t *testing.T, pool *pgxpool.Pool, serial string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM readings WHERE serial = $1`, serial).Scan(&n); err != nil {
		t.Fatalf("count readings: %v", err)
	}
	return n
}

func TestReadingStore_InsertBatch(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store := NewReadingStore(pool)

	const serial = "ITEST-0001"
	seedDevice(t, pool, serial)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	batch := []*telemetry.Reading{
		{Serial: serial, RecordedAt: base, Metric: "temperature", Value: 21.5},
		{Serial: serial, RecordedAt: base.Add(time.Minute), Metric: "temperature", Value: 21.7},
		{Serial: serial, RecordedAt: base, Metric: "humidity", Value: 60},
	}

	t.Run("全新的一批都是 inserted", func(t *testing.T) {
		got, err := store.InsertBatch(ctx, batch)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != len(batch) {
			t.Fatalf("結果長度 = %d, want %d", len(got), len(batch))
		}
		for i, r := range got {
			if r != telemetry.WriteInserted {
				t.Errorf("results[%d] = %v, want inserted", i, r)
			}
		}
		if n := countReadings(t, pool, serial); n != 3 {
			t.Errorf("資料庫裡有 %d 筆, want 3", n)
		}
	})

	t.Run("重送同一批都是 duplicate", func(t *testing.T) {
		// 這正是 ADR-0002 的冪等要求:設備因逾時重送時,唯一鍵必須重現,
		// 平台安靜地吸收。recorded_at 由設備提出才做得到。
		got, err := store.InsertBatch(ctx, batch)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for i, r := range got {
			if r != telemetry.WriteDuplicate {
				t.Errorf("results[%d] = %v, want duplicate", i, r)
			}
		}
		if n := countReadings(t, pool, serial); n != 3 {
			t.Errorf("重送之後有 %d 筆, want 3(不該長出新的)", n)
		}
	})

	t.Run("同一批裡新舊混雜", func(t *testing.T) {
		mixed := []*telemetry.Reading{
			batch[0], // 已存在
			{Serial: serial, RecordedAt: base.Add(2 * time.Minute), Metric: "temperature", Value: 22.0},
			batch[2], // 已存在
		}
		got, err := store.InsertBatch(ctx, mixed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []telemetry.WriteResult{
			telemetry.WriteDuplicate, telemetry.WriteInserted, telemetry.WriteDuplicate,
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("results[%d] = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("空批次不打資料庫", func(t *testing.T) {
		got, err := store.InsertBatch(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

// TestReadingStore_BatchIsAtomic 釘住 pgx.Batch 的隱含交易語意。
//
// SendBatch 的所有語句跑在一個隱含交易裡,所以任一筆真的失敗會讓整批回滾 ——
// 包括在它之前、已經回報「寫入成功」的那些。InsertBatch 因此必須在發現任何
// 真錯誤時整批回 error,不能回傳逐筆結果,否則就是謊報。
//
// 這個測試是那個決定的護欄:哪天有人「優化」成邊讀邊回報,這裡會紅。
func TestReadingStore_BatchIsAtomic(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store := NewReadingStore(pool)

	const serial = "ITEST-0002"
	seedDevice(t, pool, serial)

	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	got, err := store.InsertBatch(ctx, []*telemetry.Reading{
		{Serial: serial, RecordedAt: base, Metric: "temperature", Value: 1},
		// 這台設備不存在 → 外鍵違反 → 整批回滾
		{Serial: "ITEST-NO-SUCH-DEVICE", RecordedAt: base, Metric: "temperature", Value: 2},
		{Serial: serial, RecordedAt: base.Add(time.Minute), Metric: "temperature", Value: 3},
	})

	if err == nil {
		t.Fatalf("整批應該失敗,卻回了 %v", got)
	}
	if got != nil {
		t.Errorf("失敗時不該回傳逐筆結果,卻拿到 %v", got)
	}
	if n := countReadings(t, pool, serial); n != 0 {
		t.Errorf("整批應該回滾,資料庫裡卻有 %d 筆", n)
	}
}
