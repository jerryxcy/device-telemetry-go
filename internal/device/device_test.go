package device

import (
	"errors"
	"testing"
)

// Table-driven test 是 Go 最有代表性的測試風格,面試常聊。
// 這裡先給你骨架,實作 Validate 之後把 want 改成真的預期。
func TestDevice_Validate(t *testing.T) {
	tests := []struct {
		name    string
		device  Device
		wantErr error // nil 表示應該通過
	}{
		{
			name:    "valid device",
			device:  Device{Name: "sensor-01", Status: StatusActive},
			wantErr: nil,
		},
		{
			name:    "empty name",
			device:  Device{Name: "", Status: StatusActive},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "bad status",
			device:  Device{Name: "sensor-01", Status: Status("exploded")},
			wantErr: ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("TODO(day1): 實作 Device.Validate 後把這行刪掉")

			err := tt.device.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}
