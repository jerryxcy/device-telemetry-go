package telemetry

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jerryxcy/device-telemetry-go/internal/device"
)

// fakeRepo 是 Repository 的假實作 —— service 層測試不必碰資料庫。
type fakeRepo struct {
	results []WriteResult // 依序回給每一筆
	err     error

	got []*Reading // 記下實際收到什麼,有些行為只能從這裡驗證
}

var _ Repository = (*fakeRepo)(nil)

func (f *fakeRepo) InsertBatch(ctx context.Context, readings []*Reading) ([]WriteResult, error) {
	f.got = readings
	if f.err != nil {
		return nil, f.err
	}
	if f.results != nil {
		return f.results, nil
	}
	// 預設:全部視為新寫入
	out := make([]WriteResult, len(readings))
	return out, nil
}

// fakeEligibility 直接回傳設定好的結果。
type fakeEligibility struct {
	err   error
	calls int
}

var _ EligibilityChecker = (*fakeEligibility)(nil)

func (f *fakeEligibility) CheckEligibility(ctx context.Context, serial string) error {
	f.calls++
	return f.err
}

var errDeviceServiceDown = errors.New("device-service unavailable")

// newService 組一個把時鐘固定住的 Service,時鐘檢查才測得準。
func newService(t *testing.T, repo Repository, elig EligibilityChecker, now time.Time) *Service {
	t.Helper()
	s := NewService(repo, elig)
	s.now = func() time.Time { return now }
	return s
}

var fixedNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func reading(offset time.Duration, metric string, value float64) Reading {
	return Reading{RecordedAt: fixedNow.Add(offset), Metric: metric, Value: value}
}

func TestService_Submit_Eligibility(t *testing.T) {
	ctx := context.Background()
	batch := []Reading{reading(0, "temperature", 21.5)}

	t.Run("未註冊:整批拒絕,ErrNotFound 要穿透", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := newService(t, repo, &fakeEligibility{err: device.ErrNotFound}, fixedNow)

		out, err := svc.Submit(ctx, "SN-0001", batch)

		if !errors.Is(err, device.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if out != nil {
			t.Errorf("整批拒絕時不該回逐筆結果,拿到 %v", out)
		}
		if repo.got != nil {
			t.Errorf("沒資格卻還是寫了資料庫")
		}
	})

	t.Run("已停用:整批拒絕,ErrDisabled 要穿透", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := newService(t, repo, &fakeEligibility{err: device.ErrDisabled}, fixedNow)

		_, err := svc.Submit(ctx, "SN-0001", batch)

		if !errors.Is(err, device.ErrDisabled) {
			t.Fatalf("err = %v, want ErrDisabled", err)
		}
		if repo.got != nil {
			t.Errorf("沒資格卻還是寫了資料庫")
		}
	})

	t.Run("device-service 掛了:不能被誤判成沒註冊", func(t *testing.T) {
		// 這是整個 Day 2 最重要的一條界線。把「查不到答案」當成「沒註冊」
		// 會讓 device-service 的故障變成安靜地丟掉讀數。
		repo := &fakeRepo{}
		svc := newService(t, repo, &fakeEligibility{err: errDeviceServiceDown}, fixedNow)

		_, err := svc.Submit(ctx, "SN-0001", batch)

		if !errors.Is(err, errDeviceServiceDown) {
			t.Fatalf("err = %v, want errDeviceServiceDown", err)
		}
		if errors.Is(err, device.ErrNotFound) || errors.Is(err, device.ErrDisabled) {
			t.Errorf("查詢失敗被誤判成明確的拒絕:%v", err)
		}
	})

	t.Run("資格每批只查一次", func(t *testing.T) {
		elig := &fakeEligibility{}
		svc := newService(t, &fakeRepo{}, elig, fixedNow)

		_, err := svc.Submit(ctx, "SN-0001", []Reading{
			reading(0, "temperature", 1),
			reading(time.Minute, "temperature", 2),
			reading(2*time.Minute, "humidity", 3),
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if elig.calls != 1 {
			t.Errorf("查了 %d 次資格, want 1", elig.calls)
		}
	})
}

func TestService_Submit_PerReading(t *testing.T) {
	ctx := context.Background()

	t.Run("寫入結果對應回原本的位置", func(t *testing.T) {
		repo := &fakeRepo{results: []WriteResult{WriteInserted, WriteDuplicate}}
		svc := newService(t, repo, &fakeEligibility{}, fixedNow)

		out, err := svc.Submit(ctx, "SN-0001", []Reading{
			reading(0, "temperature", 1),
			reading(time.Minute, "temperature", 2),
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []Status{StatusAccepted, StatusDuplicate}
		for i := range want {
			if out[i].Status != want[i] {
				t.Errorf("out[%d].Status = %v, want %v", i, out[i].Status, want[i])
			}
		}
	})

	t.Run("被拒的不送進資料庫,其餘照常寫入", func(t *testing.T) {
		// 中間兩筆會被逐筆拒絕,只有第 0 與第 3 筆該進 repo。
		repo := &fakeRepo{results: []WriteResult{WriteInserted, WriteInserted}}
		svc := newService(t, repo, &fakeEligibility{}, fixedNow)

		out, err := svc.Submit(ctx, "SN-0001", []Reading{
			reading(0, "temperature", 1),                // ok
			reading(0, "", 2),                           // metric 空 → invalid
			reading(365*24*time.Hour, "temperature", 3), // 時鐘超前 → clock
			reading(-time.Hour, "humidity", 4),          // ok
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []Status{StatusAccepted, StatusInvalid, StatusClockOutOfRange, StatusAccepted}
		for i := range want {
			if out[i].Status != want[i] {
				t.Errorf("out[%d].Status = %v, want %v", i, out[i].Status, want[i])
			}
		}
		// 位置要對得回去 —— 這是最容易寫錯的地方
		if len(repo.got) != 2 {
			t.Fatalf("送進 repo %d 筆, want 2", len(repo.got))
		}
		if repo.got[0].Metric != "temperature" || repo.got[1].Metric != "humidity" {
			t.Errorf("送進 repo 的是 %q / %q, want temperature / humidity",
				repo.got[0].Metric, repo.got[1].Metric)
		}

		// 被拒的兩筆要帶得出原因
		if !errors.Is(out[1].Err, ErrInvalidReading) {
			t.Errorf("out[1].Err = %v, want ErrInvalidReading", out[1].Err)
		}
		if !errors.Is(out[2].Err, ErrClockOutOfRange) {
			t.Errorf("out[2].Err = %v, want ErrClockOutOfRange", out[2].Err)
		}
	})

	t.Run("全部被拒時不打資料庫", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := newService(t, repo, &fakeEligibility{}, fixedNow)

		out, err := svc.Submit(ctx, "SN-0001", []Reading{
			reading(0, "", 1),
			reading(0, "temperature", math.NaN()),
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out[0].Status != StatusInvalid || out[1].Status != StatusInvalid {
			t.Errorf("out = %v, want 兩筆都是 invalid", out)
		}
		if repo.got != nil {
			t.Errorf("沒有東西要寫卻打了資料庫")
		}
	})

	t.Run("Serial 由批次決定,不採信逐筆帶的值", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := newService(t, repo, &fakeEligibility{}, fixedNow)

		r := reading(0, "temperature", 1)
		r.Serial = "SN-想冒充的"

		if _, err := svc.Submit(ctx, "SN-0001", []Reading{r}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repo.got[0].Serial != "SN-0001" {
			t.Errorf("寫入的 serial = %q, want SN-0001", repo.got[0].Serial)
		}
	})

	t.Run("寫入失敗時不回逐筆結果", func(t *testing.T) {
		// InsertBatch 整批回滾時逐筆判定會說謊,所以只能整批回 error。
		repo := &fakeRepo{err: errors.New("batch rolled back")}
		svc := newService(t, repo, &fakeEligibility{}, fixedNow)

		out, err := svc.Submit(ctx, "SN-0001", []Reading{reading(0, "temperature", 1)})

		if err == nil {
			t.Fatal("expected error")
		}
		if out != nil {
			t.Errorf("寫入失敗卻回了 %v", out)
		}
	})
}

// Repository 若違反契約(結果數量對不上、回了未知的 WriteResult),
// Submit 必須整批失敗而不是猜。這兩條在正常運作下走不到,
// 但它們是「不要默默給出錯誤答案」的最後一道防線。
func TestService_Submit_RepositoryMisbehaves(t *testing.T) {
	ctx := context.Background()
	batch := []Reading{reading(0, "temperature", 1), reading(time.Minute, "temperature", 2)}

	t.Run("回傳的結果數量對不上", func(t *testing.T) {
		repo := &fakeRepo{results: []WriteResult{WriteInserted}} // 送 2 筆卻只回 1 筆
		svc := newService(t, repo, &fakeEligibility{}, fixedNow)

		out, err := svc.Submit(ctx, "SN-0001", batch)

		if err == nil {
			t.Fatalf("結果數量對不上時應該失敗,卻回了 %v", out)
		}
		if out != nil {
			t.Errorf("got %v, want nil", out)
		}
	})

	t.Run("回傳未知的 WriteResult", func(t *testing.T) {
		repo := &fakeRepo{results: []WriteResult{WriteInserted, WriteResult(99)}}
		svc := newService(t, repo, &fakeEligibility{}, fixedNow)

		out, err := svc.Submit(ctx, "SN-0001", batch)

		if err == nil {
			t.Fatalf("未知的寫入結果應該失敗,卻回了 %v", out)
		}
		if out != nil {
			t.Errorf("got %v, want nil", out)
		}
	})
}

func TestService_Submit_Edge(t *testing.T) {
	ctx := context.Background()

	t.Run("空 serial 直接擋掉,不查資格", func(t *testing.T) {
		elig := &fakeEligibility{}
		svc := newService(t, &fakeRepo{}, elig, fixedNow)

		_, err := svc.Submit(ctx, "  ", []Reading{reading(0, "temperature", 1)})

		if !errors.Is(err, ErrInvalidReading) {
			t.Fatalf("err = %v, want ErrInvalidReading", err)
		}
		if elig.calls != 0 {
			t.Errorf("serial 不合法卻查了資格")
		}
	})

	t.Run("空批次不查資格也不打資料庫", func(t *testing.T) {
		elig := &fakeEligibility{}
		repo := &fakeRepo{}
		svc := newService(t, repo, elig, fixedNow)

		out, err := svc.Submit(ctx, "SN-0001", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != nil {
			t.Errorf("got %v, want nil", out)
		}
		if elig.calls != 0 || repo.got != nil {
			t.Errorf("空批次不該碰任何下游")
		}
	})
}

func TestStatus_String(t *testing.T) {
	for _, tt := range []struct {
		in   Status
		want string
	}{
		{StatusAccepted, "accepted"},
		{StatusDuplicate, "duplicate"},
		{StatusClockOutOfRange, "clock_out_of_range"},
		{StatusInvalid, "invalid"},
		{Status(99), "Status(99)"},
	} {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", int(tt.in), got, tt.want)
		}
	}
}
