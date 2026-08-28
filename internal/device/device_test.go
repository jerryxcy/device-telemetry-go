package device

import (
	"errors"
	"strings"
	"testing"
)

// Table-driven test 是 Go 最有代表性的測試風格,面試常聊。

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
		{
			name:    "serial too long",
			input:   RegisterInput{Serial: strings.Repeat("A", 129), Name: "溫度計"},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "serial at limit is ok",
			input:   RegisterInput{Serial: strings.Repeat("A", 128), Name: "溫度計"},
			wantErr: nil,
		},
		{
			name:    "whitespace-only serial",
			input:   RegisterInput{Serial: "   ", Name: "溫度計"},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "whitespace-only name",
			input:   RegisterInput{Serial: "SN-0001", Name: "   "},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "name too long",
			input:   RegisterInput{Serial: "SN-0001", Name: strings.Repeat("溫", 129)},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "name at limit is ok",
			input:   RegisterInput{Serial: "SN-0001", Name: strings.Repeat("溫", 128)},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
		{
			name:    "name too long",
			input:   UpdateInput{Name: strings.Repeat("溫", 129), Enabled: true},
			wantErr: ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
