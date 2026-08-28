// Package device 是 domain 層:定義核心型別與業務錯誤。
// 這一層不知道 HTTP,也不知道 SQL。
//
// 詞彙定義見專案根目錄的 CONTEXT.md。
package device

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// 業務錯誤。上層用 errors.Is 判斷,不要用字串比對。
var (
	ErrNotFound     = errors.New("device not found")
	ErrInvalidInput = errors.New("invalid input")

	// ErrAlreadyExists 只在 Repository 與 Service 之間流通。
	// 註冊是冪等的,所以 Service 收到它之後會改抓現有那筆,
	// 這個錯誤永遠不該冒到 HTTP 層。
	ErrAlreadyExists = errors.New("device already exists")

	// ErrDisabled 供 Day 2 的上報准入使用:設備存在,但平台不收它的讀數。
	ErrDisabled = errors.New("device is disabled")
)

// maxFieldLen 是文字欄位的長度上限,以字元(rune)計,不是 byte。
const maxFieldLen = 128

// Device 是核心 domain 型別。
//
// Enabled 只有兩個狀態,所以是 bool 而非 enum —— 非法值在型別上不存在,
// 不需要任何驗證函式去擋。
type Device struct {
	Serial    string    `json:"serial"`
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RegisterInput 是註冊時外界能提供的全部欄位。
//
// 刻意不直接拿 Device 當輸入:否則呼叫端可以偽造 CreatedAt,
// 或在註冊當下就把自己設成 enabled=false。
type RegisterInput struct {
	Serial   string `json:"serial"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

// validateName 檢查給人看的標籤欄位。Register 和 Update 共用同一條規則。
func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if utf8.RuneCountInString(name) > maxFieldLen {
		return fmt.Errorf("%w: name exceeds %d characters", ErrInvalidInput, maxFieldLen)
	}
	return nil
}

// Validate 檢查註冊輸入。
//
//   - Serial 不可為空,長度上限 128 個字元
//   - Name 不可為空,長度上限 128 個字元
//
// 所有錯誤都滿足 errors.Is(err, ErrInvalidInput)。
func (in RegisterInput) Validate() error {
	if strings.TrimSpace(in.Serial) == "" {
		return fmt.Errorf("%w: serial is required", ErrInvalidInput)
	}
	if utf8.RuneCountInString(in.Serial) > maxFieldLen {
		return fmt.Errorf("%w: serial exceeds %d characters", ErrInvalidInput, maxFieldLen)
	}

	return validateName(in.Name)
}

// UpdateInput 是 PUT 能改的全部欄位。
//
// 注意沒有 Serial —— 那是身分,不能改。
type UpdateInput struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	Enabled  bool   `json:"enabled"`
}

// Validate 檢查更新輸入。
//
// Enabled 是 bool,沒有非法值,所以只需要檢查 Name。
func (in UpdateInput) Validate() error {
	return validateName(in.Name)
}
