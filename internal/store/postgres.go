// Package store 是 repository 層:唯一知道 SQL 長什麼樣的地方。
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jerryxcy/device-telemetry-go/internal/device"
)

// DeviceStore 實作 device.Repository。
// 注意這裡沒有任何 "implements" 宣告 —— Go 的 interface 是隱式滿足的。
type DeviceStore struct {
	pool *pgxpool.Pool
}

func NewDeviceStore(pool *pgxpool.Pool) *DeviceStore {
	return &DeviceStore{pool: pool}
}

// TODO(day1): 實作以下五個方法。
//
// 重點:
//  1. GetBySerial 查不到時,把 pgx.ErrNoRows 轉成 device.ErrNotFound ——
//     不要讓 pgx 的錯誤型別洩漏到上層,那會讓 service 層依賴資料庫套件
//  2. Create 撞到主鍵衝突時轉成 device.ErrAlreadyExists。
//     判斷方式:errors.As 取出 *pgconn.PgError,檢查 Code == "23505"
//  3. 每個 query 都要吃 ctx,pgx 會在 ctx 取消時中斷查詢

func (s *DeviceStore) Create(ctx context.Context, d *device.Device) error {
	panic("not implemented")
}

func (s *DeviceStore) GetBySerial(ctx context.Context, serial string) (*device.Device, error) {
	panic("not implemented")
}

func (s *DeviceStore) List(ctx context.Context, limit, offset int) ([]*device.Device, error) {
	panic("not implemented")
}

func (s *DeviceStore) Update(ctx context.Context, d *device.Device) error {
	panic("not implemented")
}

// Delete 刪除設備及其讀數。這是本專案唯一需要交易的操作。
//
// TODO(day1): 自己實作。骨架長這樣:
//
//	tx, err := s.pool.Begin(ctx)
//	if err != nil { ... }
//	defer tx.Rollback(ctx)   // Commit 之後再 Rollback 是無害的 no-op,
//	                         // 所以這行一定要寫,它負責所有提早 return 的路徑
//
//	// 先刪子表:readings 有 FK 指向 devices,順序反了會撞 FK 錯誤
//	tx.Exec(ctx, `DELETE FROM readings WHERE serial = $1`, serial)
//	tx.Exec(ctx, `DELETE FROM devices  WHERE serial = $1`, serial)
//
//	return tx.Commit(ctx)
//
// 為什麼不用 ON DELETE CASCADE:見 docs/adr/0003。
func (s *DeviceStore) Delete(ctx context.Context, serial string) error {
	panic("not implemented")
}
