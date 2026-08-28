package device

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// 分頁邊界。定在 service 層,因為「一次吐幾筆」是業務決策,不是儲存細節。
const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// Repository 是 device service 對儲存層的需求。
//
// 注意:interface 定義在「使用端」(這裡),不是定義在實作端(store 套件)。
// 這是 Go 跟 Java/Spring 最大的思維差異之一 —— 由消費者宣告它需要什麼,
// 實作者不需要 import 這個套件,也不需要寫 "implements"。
type Repository interface {
	Create(ctx context.Context, d *Device) error
	GetBySerial(ctx context.Context, serial string) (*Device, error)
	List(ctx context.Context, limit, offset int) ([]*Device, error)
	Update(ctx context.Context, d *Device) error
	// Delete 連同該設備的讀數一起刪除,且必須是原子的。
	Delete(ctx context.Context, serial string) error
}

// Service 持有業務邏輯。
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// 共同重點:
//   1. ctx 一定要往下傳,不要用 context.Background()
//   2. 錯誤往上傳時用 %w 包裝,保留 errors.Is 的判斷能力
//   3. 業務規則寫在這層,不要漏到 handler 或 repository

// Register 註冊設備。冪等:同一個 Serial 重複註冊視為同一次註冊,
// 回傳既有那筆,不會覆蓋平台上已改過的 Name/Location。
//
// 新註冊的設備一律 Enabled=true,不接受外界指定。
func (s *Service) Register(ctx context.Context, in RegisterInput) (*Device, error) {

	if err := in.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	d := &Device{
		Serial:    in.Serial,
		Name:      in.Name,
		Location:  in.Location,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.Create(ctx, d); err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			existing, err := s.repo.GetBySerial(ctx, in.Serial)
			if err != nil {
				return nil, fmt.Errorf("register %s: %w", in.Serial, err)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("register %s: %w", in.Serial, err)
	}
	return d, nil
}

// Get 依 Serial 取單一設備。找不到時回傳包裝過的 ErrNotFound。
func (s *Service) Get(ctx context.Context, serial string) (*Device, error) {
	d, err := s.repo.GetBySerial(ctx, serial)
	if err != nil {
		return nil, fmt.Errorf("get device %s: %w", serial, err)
	}
	return d, nil
}

// List 回傳設備清單。
//
// limit 未指定或超出範圍時會被夾到 [1, 200],預設 50。
func (s *Service) List(ctx context.Context, limit, offset int) ([]*Device, error) {

	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if offset < 0 {
		offset = 0
	}

	devices, err := s.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list devices (limit=%d, offset=%d): %w", limit, offset, err)
	}
	return devices, nil
}

// Update 以 in 的內容完整覆寫 Name / Location / Enabled。
// 設備不存在時回傳包裝過的 ErrNotFound。
func (s *Service) Update(ctx context.Context, serial string, in UpdateInput) (*Device, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	d, err := s.repo.GetBySerial(ctx, serial)
	if err != nil {
		return nil, fmt.Errorf("update device %s: %w", serial, err)
	}

	d.Name = in.Name
	d.Location = in.Location
	d.Enabled = in.Enabled
	d.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, d); err != nil {
		return nil, fmt.Errorf("update device %s: %w", serial, err)
	}
	return d, nil
}

// Delete 刪除設備,連同它的讀數。
//
// 冪等:刪一台不存在的設備不是錯誤,一樣回 nil。
func (s *Service) Delete(ctx context.Context, serial string) error {
	if err := s.repo.Delete(ctx, serial); err != nil {
		return fmt.Errorf("delete device %s: %w", serial, err)
	}
	return nil
}
