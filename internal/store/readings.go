package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jerryxcy/device-telemetry-go/internal/telemetry"
)

// ReadingStore 實作 telemetry.Repository。
type ReadingStore struct {
	pool *pgxpool.Pool
}

// 編譯期斷言,對照 DeviceStore 那邊的寫法。
var _ telemetry.Repository = (*ReadingStore)(nil)

func NewReadingStore(pool *pgxpool.Pool) *ReadingStore {
	return &ReadingStore{pool: pool}
}

// insertReading 靠 RETURNING 分辨「真的寫入」與「被 ON CONFLICT 吸收」。
//
// ON CONFLICT DO NOTHING 本身不回報有沒有作用 —— 撞到重複時它安靜地什麼都不做,
// Exec 回傳的 RowsAffected 是 0 但那也可能是別的原因。加上 RETURNING 之後:
// 真的插入了才會有一列回來,被吸收就是零列(也就是 pgx.ErrNoRows)。
//
// 這是 ACCEPTED 與 DUPLICATE 分得開的唯一依據。
const insertReading = `
	INSERT INTO readings (serial, recorded_at, metric, value)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT DO NOTHING
	RETURNING serial`

// InsertBatch 寫入一批讀數,逐筆回報是新寫入還是重複。
// 回傳的 slice 與 readings 等長且順序一致。
//
// 整批共用一次來回:pgx.Batch 把所有語句一起送出,而不是一筆一個 round trip。
//
// 代價是**整批是一個隱含交易**。任何一筆撞到真正的錯誤(例如設備在檢查資格與
// 寫入之間被刪掉,撞上外鍵),整批都會回滾 —— 連前面那些「看起來成功」的也不例外。
// 所以這裡掃完全部結果才做判斷:只要有一筆真錯誤,就整批回 error,不回傳任何
// 逐筆結果,免得謊報成功。
//
// 這樣做是安全的,因為寫入是冪等的:設備重送整批,已存在的會被吸收。
//
// received_at 交給資料庫的 DEFAULT now() —— 它是平台蓋的章,而且在同一個交易裡
// now() 對每一筆都相同,語意上正好是「這批一起到達的時間」。
func (s *ReadingStore) InsertBatch(ctx context.Context, readings []*telemetry.Reading) ([]telemetry.WriteResult, error) {
	if len(readings) == 0 {
		return nil, nil
	}

	batch := &pgx.Batch{}
	for _, r := range readings {
		batch.Queue(insertReading, r.Serial, r.RecordedAt, r.Metric, r.Value)
	}

	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()

	results := make([]telemetry.WriteResult, len(readings))
	var firstErr error
	for i := range readings {
		var serial string
		switch err := br.QueryRow().Scan(&serial); {
		case err == nil:
			results[i] = telemetry.WriteInserted
		case errors.Is(err, pgx.ErrNoRows):
			results[i] = telemetry.WriteDuplicate
		case firstErr == nil:
			// 記下第一個真錯誤,但仍要把剩下的結果讀完 —— BatchResults
			// 必須讀到底才能安全關閉,不然連線狀態會亂掉。
			firstErr = fmt.Errorf("insert reading %s/%s: %w", readings[i].Serial, readings[i].Metric, err)
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}
