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

// TODO(day1): 實作以下方法。
//
// 重點:
//   1. GetByID 查不到時,把 pgx.ErrNoRows 轉成 device.ErrNotFound ——
//      不要讓 pgx 的錯誤型別洩漏到上層,那會讓 service 層依賴資料庫套件
//   2. Create 撞到 unique constraint 時轉成 device.ErrAlreadyExists
//   3. 每個 query 都要吃 ctx,pgx 會在 ctx 取消時中斷查詢

func (s *DeviceStore) Create(ctx context.Context, d *device.Device) error {
	panic("not implemented")
}

func (s *DeviceStore) GetByID(ctx context.Context, id string) (*device.Device, error) {
	panic("not implemented")
}

func (s *DeviceStore) List(ctx context.Context, limit, offset int) ([]*device.Device, error) {
	panic("not implemented")
}

func (s *DeviceStore) Update(ctx context.Context, d *device.Device) error {
	panic("not implemented")
}

func (s *DeviceStore) Delete(ctx context.Context, id string) error {
	panic("not implemented")
}
