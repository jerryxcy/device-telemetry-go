//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jerryxcy/device-telemetry-go/internal/device"
)

// DeviceStore 的五個方法。單元測試那邊用假的 Repository 驗業務邏輯,
// 這裡驗的是 SQL 本身 —— 欄位順序、錯誤碼轉換、交易,那些只有真的
// 跑一次 PostgreSQL 才知道對不對。

func newDevice(serial string) *device.Device {
	now := time.Now().UTC().Truncate(time.Microsecond) // PostgreSQL 的精度是微秒
	return &device.Device{
		Serial:    serial,
		Name:      "整合測試機",
		Location:  "1F",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// cleanupDevices 在測試前後都清一次,前面那次是為了上一輪中途失敗的殘留。
func cleanupDevices(t *testing.T, pool *pgxpool.Pool, serials ...string) {
	t.Helper()
	clean := func() {
		for _, s := range serials {
			pool.Exec(context.Background(), `DELETE FROM readings WHERE serial = $1`, s)
			pool.Exec(context.Background(), `DELETE FROM devices WHERE serial = $1`, s)
		}
	}
	clean()
	t.Cleanup(clean)
}

func TestDeviceStore_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store := NewDeviceStore(pool)
	cleanupDevices(t, pool, "DEV-0001")

	d := newDevice("DEV-0001")

	t.Run("建立", func(t *testing.T) {
		if err := store.Create(ctx, d); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("每個欄位都要對得回來", func(t *testing.T) {
		// 這一項守的是 SELECT 的欄位順序與 Scan 的參數順序 ——
		// 兩份清單各自正確但順序對不上時,編譯器與 vet 都不會有意見,
		// 只會靜默錯位。name 與 location 都是 TEXT,交換不會噴錯。
		got, err := store.GetBySerial(ctx, "DEV-0001")
		if err != nil {
			t.Fatalf("GetBySerial: %v", err)
		}
		if got.Serial != d.Serial || got.Name != d.Name || got.Location != d.Location {
			t.Errorf("got %+v, want serial/name/location = %s/%s/%s",
				got, d.Serial, d.Name, d.Location)
		}
		if !got.Enabled {
			t.Errorf("Enabled = false, want true")
		}
		if !got.CreatedAt.Equal(d.CreatedAt) || !got.UpdatedAt.Equal(d.UpdatedAt) {
			t.Errorf("時間對不上:got %v / %v, want %v", got.CreatedAt, got.UpdatedAt, d.CreatedAt)
		}
	})

	t.Run("讀回來的時間是 UTC", func(t *testing.T) {
		// pgx 解 timestamptz 用的是行程的本地時區,scanDevice 負責轉成 UTC。
		// 少了那一步,同一個欄位會依來源序列化成兩種格式。
		got, _ := store.GetBySerial(ctx, "DEV-0001")
		if got.CreatedAt.Location() != time.UTC {
			t.Errorf("CreatedAt 的時區是 %v, want UTC", got.CreatedAt.Location())
		}
	})

	t.Run("重複建立回 ErrAlreadyExists", func(t *testing.T) {
		// 主鍵衝突是 SQLSTATE 23505,由 errors.As 取出 *pgconn.PgError 判斷。
		err := store.Create(ctx, newDevice("DEV-0001"))
		if !errors.Is(err, device.ErrAlreadyExists) {
			t.Fatalf("err = %v, want ErrAlreadyExists", err)
		}
	})

	t.Run("查不到回 ErrNotFound", func(t *testing.T) {
		// pgx.ErrNoRows 必須在 store 這層轉掉,不能洩漏到 domain。
		_, err := store.GetBySerial(ctx, "DEV-NEVER-EXISTED")
		if !errors.Is(err, device.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestDeviceStore_List(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store := NewDeviceStore(pool)

	serials := []string{"LIST-0003", "LIST-0001", "LIST-0002"} // 刻意亂序建立
	cleanupDevices(t, pool, serials...)
	for _, s := range serials {
		if err := store.Create(ctx, newDevice(s)); err != nil {
			t.Fatalf("Create %s: %v", s, err)
		}
	}

	// 只看我們自己建的那幾台 —— 資料庫裡可能還有別的。
	onlyOurs := func(ds []*device.Device) []string {
		var out []string
		for _, d := range ds {
			if len(d.Serial) > 5 && d.Serial[:5] == "LIST-" {
				out = append(out, d.Serial)
			}
		}
		return out
	}

	t.Run("依 serial 排序", func(t *testing.T) {
		// 沒有 ORDER BY 的話 LIMIT/OFFSET 翻頁會重複或漏掉,而且不會報錯。
		got, err := store.List(ctx, 100, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		want := []string{"LIST-0001", "LIST-0002", "LIST-0003"}
		ours := onlyOurs(got)
		if len(ours) != len(want) {
			t.Fatalf("撈到 %v, want %v", ours, want)
		}
		for i := range want {
			if ours[i] != want[i] {
				t.Errorf("第 %d 筆是 %s, want %s", i, ours[i], want[i])
			}
		}
	})

	t.Run("limit 生效", func(t *testing.T) {
		got, err := store.List(ctx, 2, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("拿到 %d 筆, want 2", len(got))
		}
	})

	t.Run("offset 生效", func(t *testing.T) {
		first, _ := store.List(ctx, 1, 0)
		second, _ := store.List(ctx, 1, 1)
		if len(first) != 1 || len(second) != 1 {
			t.Fatalf("每頁應各有 1 筆")
		}
		if first[0].Serial == second[0].Serial {
			t.Errorf("offset 沒生效,兩頁都是 %s", first[0].Serial)
		}
	})

	t.Run("查無資料回空 slice 而非 nil", func(t *testing.T) {
		// nil 會序列化成 JSON 的 null,空 slice 才是 []。
		got, err := store.List(ctx, 10, 100000)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if got == nil {
			t.Errorf("got nil, want 空 slice")
		}
		if len(got) != 0 {
			t.Errorf("got %d 筆, want 0", len(got))
		}
	})
}

func TestDeviceStore_Update(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store := NewDeviceStore(pool)
	cleanupDevices(t, pool, "UPD-0001")

	d := newDevice("UPD-0001")
	if err := store.Create(ctx, d); err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("三個欄位都更新,created_at 不動", func(t *testing.T) {
		updated := *d
		updated.Name = "改過的名字"
		updated.Location = "3F 機房"
		updated.Enabled = false
		updated.UpdatedAt = time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)

		if err := store.Update(ctx, &updated); err != nil {
			t.Fatalf("Update: %v", err)
		}

		got, err := store.GetBySerial(ctx, "UPD-0001")
		if err != nil {
			t.Fatalf("GetBySerial: %v", err)
		}
		if got.Name != updated.Name || got.Location != updated.Location {
			t.Errorf("name/location = %q/%q, want %q/%q",
				got.Name, got.Location, updated.Name, updated.Location)
		}
		if got.Enabled {
			t.Errorf("Enabled = true, want false")
		}
		if !got.UpdatedAt.Equal(updated.UpdatedAt) {
			t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, updated.UpdatedAt)
		}
		// created_at 不在 SET 裡,更新後必須維持原值。
		if !got.CreatedAt.Equal(d.CreatedAt) {
			t.Errorf("CreatedAt 被改動了:%v, want %v", got.CreatedAt, d.CreatedAt)
		}
	})

	t.Run("更新不存在的設備回 ErrNotFound", func(t *testing.T) {
		// UPDATE 匹配不到列在 SQL 裡不是錯誤,靠 RowsAffected() == 0 判斷。
		err := store.Update(ctx, newDevice("UPD-NEVER-EXISTED"))
		if !errors.Is(err, device.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestDeviceStore_Delete(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store := NewDeviceStore(pool)
	cleanupDevices(t, pool, "DEL-0001")

	if err := store.Create(ctx, newDevice("DEL-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// 塞兩筆讀數,驗證刪除會連同子表一起清掉。
	if _, err := pool.Exec(ctx,
		`INSERT INTO readings (serial, recorded_at, metric, value) VALUES
		 ($1, now(), 'temperature', 1), ($1, now() - interval '1 minute', 'humidity', 2)`,
		"DEL-0001"); err != nil {
		t.Fatalf("seed readings: %v", err)
	}

	t.Run("連同讀數一起刪除", func(t *testing.T) {
		// readings 對 devices 有外鍵且沒有 ON DELETE CASCADE,所以順序寫反
		// 會直接撞 FK 錯誤 —— 這一項同時驗了刪除順序。見 docs/adr/0003。
		if err := store.Delete(ctx, "DEL-0001"); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM readings WHERE serial = $1`, "DEL-0001").Scan(&n); err != nil {
			t.Fatalf("count readings: %v", err)
		}
		if n != 0 {
			t.Errorf("還剩 %d 筆讀數, want 0", n)
		}

		if _, err := store.GetBySerial(ctx, "DEL-0001"); !errors.Is(err, device.ErrNotFound) {
			t.Errorf("設備還在:%v", err)
		}
	})

	t.Run("冪等:刪不存在的設備不是錯誤", func(t *testing.T) {
		// 客戶端重送 DELETE 時必須一樣成功。store 這層刻意不檢查
		// RowsAffected —— 冪等是「不多做」換來的。
		if err := store.Delete(ctx, "DEL-0001"); err != nil {
			t.Errorf("重複刪除回了 %v, want nil", err)
		}
		if err := store.Delete(ctx, "DEL-NEVER-EXISTED"); err != nil {
			t.Errorf("刪不存在的回了 %v, want nil", err)
		}
	})
}
