// Package device 是 domain 層:定義核心型別與業務錯誤。
// 這一層不知道 HTTP,也不知道 SQL。
//
// 詞彙定義見專案根目錄的 CONTEXT.md。
package device

import (
	"errors"
	"time"
)

// 業務錯誤。上層用 errors.Is 判斷,不要用字串比對。
var (
	ErrNotFound     = errors.New("device not found")
	ErrInvalidInput = errors.New("invalid input")

	// ErrAlreadyExists 只在 Repository 與 Service 之間流通。
	// 註冊是冪等的,所以 Service 收到它之後會改抓現有那筆,
	// 這個錯誤永遠不該冒到 HTTP 層。
	ErrAlreadyExists = errors.New("device already exists")

	// ErrRetired 表示操作對象已退役。Retired 是終點狀態,不可再變更。
	ErrRetired = errors.New("device is retired")

	// ErrDisabled 供 Day 2 的上報准入使用:設備仍在服役,但平台暫時不收它的讀數。
	ErrDisabled = errors.New("device is disabled")
)

// Lifecycle 是設備的服役狀態,三值互斥。
type Lifecycle string

const (
	LifecycleEnabled  Lifecycle = "enabled"
	LifecycleDisabled Lifecycle = "disabled"
	LifecycleRetired  Lifecycle = "retired"
)

// Valid 回報是否為三個合法值之一。
//
// TODO(day1): 自己實作。
func (l Lifecycle) Valid() bool {
	return false
}

// Device 是核心 domain 型別。
type Device struct {
	Serial    string    `json:"serial"`
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	Lifecycle Lifecycle `json:"lifecycle"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RegisterInput 是註冊時外界能提供的全部欄位。
//
// 刻意不直接拿 Device 當輸入:否則呼叫端可以偽造 CreatedAt,
// 或是在註冊時就把自己設成 retired。
type RegisterInput struct {
	Serial   string `json:"serial"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

// Validate 檢查註冊輸入。
//
// TODO(day1): 自己實作。
//   - Serial 不可為空,長度上限 128
//   - Name 不可為空,長度上限 128
//   - 錯誤用 fmt.Errorf("%w: serial is required", ErrInvalidInput) 包起來,
//     上層 errors.Is(err, ErrInvalidInput) 才判斷得到
func (in RegisterInput) Validate() error {
	return nil
}

// UpdateInput 是 PUT 能改的全部欄位。
//
// 注意沒有 Serial —— 那是身分,不能改。
type UpdateInput struct {
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	Lifecycle Lifecycle `json:"lifecycle"`
}

// Validate 檢查更新輸入。
//
// TODO(day1): 自己實作。
//   - Name 不可為空,長度上限 128
//   - Lifecycle 必須 Valid()
//   - Lifecycle 不可為 LifecycleRetired —— 退役要走 DELETE,不是 PUT。
//     這條規則在這裡擋掉,handler 就不用管
func (in UpdateInput) Validate() error {
	return nil
}
