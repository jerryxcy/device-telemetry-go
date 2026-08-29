package telemetry

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestReading_Validate(t *testing.T) {
	valid := Reading{
		Serial:     "SN-0001",
		RecordedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Metric:     "temperature",
		Value:      21.5,
	}

	// with 回傳一份改過某個欄位的副本,讓每個案例只描述它關心的那個差異。
	with := func(f func(*Reading)) Reading {
		r := valid
		f(&r)
		return r
	}

	tests := []struct {
		name    string
		input   Reading
		wantErr error // nil 表示應該通過
	}{
		{"valid", valid, nil},
		{"值可以是負數", with(func(r *Reading) { r.Value = -40 }), nil},
		{"值可以是零", with(func(r *Reading) { r.Value = 0 }), nil},
		{"metric 空", with(func(r *Reading) { r.Metric = "" }), ErrInvalidReading},
		{"metric 全空白", with(func(r *Reading) { r.Metric = "   " }), ErrInvalidReading},
		{
			"metric 超長",
			with(func(r *Reading) { r.Metric = strings.Repeat("溫", maxMetricLen+1) }),
			ErrInvalidReading,
		},
		{
			"metric 剛好等於上限",
			with(func(r *Reading) { r.Metric = strings.Repeat("溫", maxMetricLen) }),
			nil,
		},
		{"recorded_at 是零值", with(func(r *Reading) { r.RecordedAt = time.Time{} }), ErrInvalidReading},
		// NaN / Inf 進得了 DOUBLE PRECISION,但之後任何聚合都會被污染成 NaN。
		{"值是 NaN", with(func(r *Reading) { r.Value = math.NaN() }), ErrInvalidReading},
		{"值是 +Inf", with(func(r *Reading) { r.Value = math.Inf(1) }), ErrInvalidReading},
		{"值是 -Inf", with(func(r *Reading) { r.Value = math.Inf(-1) }), ErrInvalidReading},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrIs(t, tt.input.Validate(), tt.wantErr)
		})
	}
}

func TestReading_CheckClock(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	at := func(d time.Duration) Reading {
		return Reading{Serial: "SN-0001", RecordedAt: now.Add(d), Metric: "temperature"}
	}

	tests := []struct {
		name    string
		input   Reading
		wantErr error
	}{
		{"就是現在", at(0), nil},
		{"稍微超前(時鐘小偏差)", at(MaxClockSkewFuture - time.Second), nil},
		{"剛好在未來上限", at(MaxClockSkewFuture), nil},
		{"超前太多", at(MaxClockSkewFuture + time.Second), ErrClockOutOfRange},
		// 設備離線期間會存著讀數,恢復連線後一次補送,所以往過去要寬。
		{"三天前的補送", at(-72 * time.Hour), nil},
		{"剛好在過去上限", at(-MaxClockAgePast), nil},
		{"太久以前", at(-MaxClockAgePast - time.Second), ErrClockOutOfRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrIs(t, tt.input.CheckClock(now), tt.wantErr)
		})
	}
}

// 兩種失敗必須分得開:資料壞了要修韌體,時鐘跑掉要校時。
func TestReading_ValidateAndCheckClockAreIndependent(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// metric 壞掉但時間正常 → 只有 Validate 該失敗
	bad := Reading{Serial: "SN-0001", RecordedAt: now, Metric: ""}
	assertErrIs(t, bad.Validate(), ErrInvalidReading)
	assertErrIs(t, bad.CheckClock(now), nil)

	// 資料正常但時間跑掉 → 只有 CheckClock 該失敗
	skewed := Reading{Serial: "SN-0001", RecordedAt: now.Add(365 * 24 * time.Hour), Metric: "temperature"}
	assertErrIs(t, skewed.Validate(), nil)
	assertErrIs(t, skewed.CheckClock(now), ErrClockOutOfRange)
}

func TestWriteResult_String(t *testing.T) {
	for _, tt := range []struct {
		in   WriteResult
		want string
	}{
		{WriteInserted, "inserted"},
		{WriteDuplicate, "duplicate"},
		{WriteResult(99), "WriteResult(99)"},
	} {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("WriteResult(%d).String() = %q, want %q", int(tt.in), got, tt.want)
		}
	}
}

// assertErrIs 對照 internal/device 的同名輔助函式。
// t.Helper() 讓失敗訊息指向呼叫端的行號,而不是這裡。
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
