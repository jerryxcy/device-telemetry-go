package device

import "context"

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

// TODO(day1): 以下五個方法自己實作。
//
// 共同重點:
//   1. ctx 一定要往下傳,不要用 context.Background()
//   2. 錯誤往上傳時用 %w 包裝,保留 errors.Is 的判斷能力
//   3. 業務規則寫在這層,不要漏到 handler 或 repository

// Register 註冊設備。冪等:同一個 Serial 重複註冊視為同一次註冊。
//
// 提示:先 Validate,再 repo.Create。若 Create 回 ErrAlreadyExists,
// 改呼叫 GetBySerial 把現有那筆回去 —— 不要覆蓋既有的 Name/Location,
// 設備重開機不該把人在平台上改過的名字蓋掉。
//
// 新註冊的設備一律 Enabled=true,不接受外界指定。
func (s *Service) Register(ctx context.Context, in RegisterInput) (*Device, error) {
	panic("not implemented")
}

func (s *Service) Get(ctx context.Context, serial string) (*Device, error) {
	panic("not implemented")
}

// List 提示:limit 要有預設值和上限,別讓呼叫端要 10000 筆你就給。
func (s *Service) List(ctx context.Context, limit, offset int) ([]*Device, error) {
	panic("not implemented")
}

// Update 更新 Name / Location / Enabled。
//
// 提示:先 Validate,再 GetBySerial 確認存在,套上新值、更新 UpdatedAt,再 repo.Update。
func (s *Service) Update(ctx context.Context, serial string, in UpdateInput) (*Device, error) {
	panic("not implemented")
}

// Delete 刪除設備,連同它的讀數。
//
// 提示:冪等 —— 刪一台不存在的設備不該是錯誤,直接回 nil。
// 真正的重點在 store 那層:兩個 DELETE 必須在同一個交易裡。
func (s *Service) Delete(ctx context.Context, serial string) error {
	panic("not implemented")
}
