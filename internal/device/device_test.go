package device

import (
	"errors"
	"testing"
)

// Table-driven test 是 Go 最有代表性的測試風格,面試常聊。
// 這裡先給你骨架,實作之後把 t.Skip 那行刪掉。

func TestRegisterInput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   RegisterInput
		wantErr error // nil 表示應該通過
	}{
		{
			name:    "valid",
			input:   RegisterInput{Serial: "SN-0001", Name: "一樓溫度計"},
			wantErr: nil,
		},
		{
			name:    "empty serial",
			input:   RegisterInput{Serial: "", Name: "一樓溫度計"},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "empty name",
			input:   RegisterInput{Serial: "SN-0001", Name: ""},
			wantErr: ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("TODO(day1): 實作 RegisterInput.Validate 後把這行刪掉")

			assertErrIs(t, tt.input.Validate(), tt.wantErr)
		})
	}
}

func TestUpdateInput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   UpdateInput
		wantErr error
	}{
		{
			name:    "valid",
			input:   UpdateInput{Name: "一樓溫度計", Enabled: true},
			wantErr: nil,
		},
		{
			name:    "disabled is just as valid",
			input:   UpdateInput{Name: "一樓溫度計", Enabled: false},
			wantErr: nil,
		},
		{
			name:    "empty name",
			input:   UpdateInput{Name: "", Enabled: true},
			wantErr: ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("TODO(day1): 實作 UpdateInput.Validate 後把這行刪掉")

			assertErrIs(t, tt.input.Validate(), tt.wantErr)
		})
	}
}

// assertErrIs 是測試輔助函式。t.Helper() 讓失敗訊息指向呼叫端的行號,
// 而不是這裡 —— 寫測試輔助函式時幾乎一定要加。
func assertErrIs(t *testing.T, got, want error) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("expected no error, got %v", got)
		}
		return
	}
	if !errors.Is(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
