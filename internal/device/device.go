// Package device 是 domain 層:定義核心型別與業務錯誤。
// 這一層不知道 HTTP,也不知道 SQL。
package device

import (
	"errors"
	"time"
)

// 業務錯誤。上層用 errors.Is 判斷,不要用字串比對。
var (
	ErrNotFound      = errors.New("device not found")
	ErrAlreadyExists = errors.New("device already exists")
	ErrInvalidInput  = errors.New("invalid input")
)

type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

// Device 是核心 domain 型別。
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate 檢查 Device 是否符合業務規則。
//
// TODO(day1): 自己實作。
//   - Name 不可為空,長度上限 128
//   - Status 必須是 StatusActive 或 StatusInactive
//   - 錯誤要用 fmt.Errorf("%w: name is required", ErrInvalidInput) 包起來,
//     這樣上層 errors.Is(err, ErrInvalidInput) 才判斷得到
func (d *Device) Validate() error {
	return nil
}
