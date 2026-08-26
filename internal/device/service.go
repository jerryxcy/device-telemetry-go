package device

import "context"

// Repository 是 device service 對儲存層的需求。
//
// 注意:interface 定義在「使用端」(這裡),不是定義在實作端(store 套件)。
// 這是 Go 跟 Java/Spring 最大的思維差異之一 —— 由消費者宣告它需要什麼,
// 實作者不需要 import 這個套件,也不需要寫 "implements"。
type Repository interface {
	Create(ctx context.Context, d *Device) error
	GetByID(ctx context.Context, id string) (*Device, error)
	List(ctx context.Context, limit, offset int) ([]*Device, error)
	Update(ctx context.Context, d *Device) error
	Delete(ctx context.Context, id string) error
}

// Service 持有業務邏輯。
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// TODO(day1): 以下五個方法自己實作。
//
// 每個方法的共同重點:
//   1. ctx 一定要往下傳,不要用 context.Background()
//   2. 錯誤往上傳時用 %w 包裝,保留 errors.Is 的判斷能力
//   3. 業務規則寫在這層,不要漏到 handler 或 repository

func (s *Service) Create(ctx context.Context, d *Device) error {
	// 提示:先 Validate,產生 ID(可用 crypto/rand 或 google/uuid),
	// 設定 CreatedAt/UpdatedAt,再交給 repo
	panic("not implemented")
}

func (s *Service) Get(ctx context.Context, id string) (*Device, error) {
	panic("not implemented")
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]*Device, error) {
	// 提示:limit 要有預設值和上限,別讓呼叫端要 10000 筆你就給
	panic("not implemented")
}

func (s *Service) Update(ctx context.Context, d *Device) error {
	panic("not implemented")
}

func (s *Service) Delete(ctx context.Context, id string) error {
	panic("not implemented")
}
