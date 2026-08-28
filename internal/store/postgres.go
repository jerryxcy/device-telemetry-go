// Package store 是 repository 層:唯一知道 SQL 長什麼樣的地方。
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jerryxcy/device-telemetry-go/internal/device"
)

// uniqueViolation 是 PostgreSQL 的 unique_violation SQLSTATE。
// devices 的主鍵是 serial,所以撞到它就代表這台設備已經註冊過。
const uniqueViolation = "23505"

// DeviceStore 實作 device.Repository。
// 注意這裡沒有任何 "implements" 宣告 —— Go 的 interface 是隱式滿足的。
type DeviceStore struct {
	pool *pgxpool.Pool
}

// 編譯期斷言:方法簽名對不上時,錯誤會指在這一行,
// 而不是等到 main 組裝 Service 的時候才爆。
var _ device.Repository = (*DeviceStore)(nil)

func NewDeviceStore(pool *pgxpool.Pool) *DeviceStore {
	return &DeviceStore{pool: pool}
}

// 這一層要守的三條規則:
//  1. pgx 的錯誤型別不可以洩漏到上層 —— pgx.ErrNoRows 轉成 device.ErrNotFound,
//     23505 轉成 device.ErrAlreadyExists。否則 service 層會反過來依賴資料庫套件。
//  2. 每個 query 都要吃 ctx,pgx 會在 ctx 取消時中斷查詢。
//  3. 參數一律走 $N placeholder,不要字串拼接 SQL。

func (s *DeviceStore) Create(ctx context.Context, d *device.Device) error {
	const q = `INSERT INTO devices (serial, name, location, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := s.pool.Exec(ctx, q,
		d.Serial, d.Name, d.Location, d.Enabled, d.CreatedAt, d.UpdatedAt)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == uniqueViolation {
			return device.ErrAlreadyExists
		}
		return fmt.Errorf("create device %s: %w", d.Serial, err)
	}
	return nil
}

// scanner 讓單列的 pgx.Row 和多列的 pgx.Rows 共用同一段掃描邏輯。
type scanner interface {
	Scan(dest ...any) error
}

// scanDevice 掃一列進 Device,並把兩個時間統一成 UTC。
//
// pgx 解 timestamptz 時給的是行程的本地時區,不轉的話同一個欄位會因為
// 「service 剛算出來的」還是「從資料庫讀回來的」而序列化成兩種格式。
func scanDevice(src scanner, d *device.Device) error {
	if err := src.Scan(
		&d.Serial, &d.Name, &d.Location, &d.Enabled, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return err
	}
	d.CreatedAt = d.CreatedAt.UTC()
	d.UpdatedAt = d.UpdatedAt.UTC()
	return nil
}

func (s *DeviceStore) GetBySerial(ctx context.Context, serial string) (*device.Device, error) {
	const q = `SELECT serial, name, location, enabled, created_at, updated_at
		FROM devices
		WHERE serial = $1`

	var d device.Device
	if err := scanDevice(s.pool.QueryRow(ctx, q, serial), &d); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, device.ErrNotFound
		}
		return nil, fmt.Errorf("get device %s: %w", serial, err)
	}
	return &d, nil
}

func (s *DeviceStore) List(ctx context.Context, limit, offset int) ([]*device.Device, error) {
	const q = `SELECT serial, name, location, enabled, created_at, updated_at
		FROM devices
		ORDER BY serial
		LIMIT $1 OFFSET $2`

	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	devices := make([]*device.Device, 0, limit)
	for rows.Next() {
		var d device.Device
		if err := scanDevice(rows, &d); err != nil {
			return nil, fmt.Errorf("list devices: scan: %w", err)
		}
		devices = append(devices, &d)
	}
	// rows.Next() 回 false 有兩種意思:讀完了、或中途出錯。迴圈本身分不出來,
	// 少了這個檢查會靜默回傳一份不完整的清單。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	return devices, nil
}

func (s *DeviceStore) Update(ctx context.Context, d *device.Device) error {
	const q = `UPDATE devices
		SET name = $2, location = $3, enabled = $4, updated_at = $5
		WHERE serial = $1`

	tag, err := s.pool.Exec(ctx, q, d.Serial, d.Name, d.Location, d.Enabled, d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update device %s: %w", d.Serial, err)
	}
	if tag.RowsAffected() == 0 {
		return device.ErrNotFound
	}
	return nil
}

// Delete 刪除設備及其讀數。這是本專案唯一需要交易的操作。
//
// 冪等:刪一台不存在的設備不是錯誤,所以刻意不檢查 RowsAffected。
//
// 為什麼不用 ON DELETE CASCADE:見 docs/adr/0003。
func (s *DeviceStore) Delete(ctx context.Context, serial string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("delete device %s: begin: %w", serial, err)
	}
	// Commit 之後再 Rollback 是無害的 no-op,所以這行負責所有提早 return 的路徑。
	defer tx.Rollback(ctx)

	// 先刪子表:readings 有 FK 指向 devices,順序反了會撞 FK 錯誤。
	if _, err := tx.Exec(ctx, `DELETE FROM readings WHERE serial = $1`, serial); err != nil {
		return fmt.Errorf("delete device %s: readings: %w", serial, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM devices WHERE serial = $1`, serial); err != nil {
		return fmt.Errorf("delete device %s: devices: %w", serial, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("delete device %s: commit: %w", serial, err)
	}
	return nil
}
