package device

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// fakeRepo 是 Repository 的假實作 —— service 層測試不需要碰資料庫。
//
// 這正是「interface 定義在使用端」的回報:Service 只認得 Repository,
// 所以任何有那五個方法的東西都能塞進去。
//
// 每個欄位分成兩類:
//   - xxxErr / xxxResult:設定這次呼叫要回什麼
//   - gotXxx:記下 repository 實際收到什麼。有些行為(例如 List 的夾值)
//     只能從「repository 收到什麼參數」來驗證,看回傳值是看不出來的。
type fakeRepo struct {
	createErr  error
	getDevice  *Device
	getErr     error
	listResult []*Device
	listErr    error
	updateErr  error
	deleteErr  error

	gotCreate *Device
	gotUpdate *Device
	gotLimit  int
	gotOffset int
	gotDelete string
	getCalls  int
}

// 編譯期斷言:簽名對不上時,錯誤會指在這一行,而不是等到組裝的時候才爆。
var _ Repository = (*fakeRepo)(nil)

func (f *fakeRepo) Create(ctx context.Context, d *Device) error {
	f.gotCreate = d
	return f.createErr
}

func (f *fakeRepo) GetBySerial(ctx context.Context, serial string) (*Device, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getDevice == nil {
		return nil, ErrNotFound
	}
	// 回傳副本,模擬每次查詢都拿到新的一份。
	// 直接回 f.getDevice 的話,Service 改欄位會連測試資料一起改掉,
	// 斷言就分不出「改對了」還是「本來就長這樣」。
	cp := *f.getDevice
	return &cp, nil
}

func (f *fakeRepo) List(ctx context.Context, limit, offset int) ([]*Device, error) {
	f.gotLimit, f.gotOffset = limit, offset
	return f.listResult, f.listErr
}

func (f *fakeRepo) Update(ctx context.Context, d *Device) error {
	f.gotUpdate = d
	return f.updateErr
}

func (f *fakeRepo) Delete(ctx context.Context, serial string) error {
	f.gotDelete = serial
	return f.deleteErr
}

// errDBDown 代表「非業務性的底層錯誤」。用具名變數而不是每次 errors.New,
// 這樣斷言時可以直接 errors.Is 比對。
var errDBDown = errors.New("connection refused")

func TestService_Register(t *testing.T) {
	ctx := context.Background()
	validInput := RegisterInput{Serial: "SN-0001", Name: "一樓溫度計", Location: "1F"}

	t.Run("新設備一律 Enabled=true", func(t *testing.T) {
		repo := &fakeRepo{}
		d, err := NewService(repo).Register(ctx, validInput)

		assertErrIs(t, err, nil)
		if !d.Enabled {
			t.Errorf("Enabled = false, 新註冊的設備應該一律是 true")
		}
		if repo.gotCreate == nil || !repo.gotCreate.Enabled {
			t.Errorf("傳給 repo.Create 的設備 Enabled 應該是 true")
		}
		if d.CreatedAt.IsZero() || !d.CreatedAt.Equal(d.UpdatedAt) {
			t.Errorf("CreatedAt/UpdatedAt 應該非零且相等, got %v / %v", d.CreatedAt, d.UpdatedAt)
		}
	})

	t.Run("冪等:重複註冊回傳既有那筆,不覆蓋 Name/Location", func(t *testing.T) {
		existing := &Device{
			Serial:   "SN-0001",
			Name:     "人在平台上改過的名字",
			Location: "3F 機房",
			Enabled:  false,
		}
		repo := &fakeRepo{createErr: ErrAlreadyExists, getDevice: existing}

		d, err := NewService(repo).Register(ctx, validInput)

		assertErrIs(t, err, nil)
		if d.Name != existing.Name {
			t.Errorf("Name = %q, 不該被 RegisterInput 覆蓋成 %q", d.Name, validInput.Name)
		}
		if d.Location != existing.Location {
			t.Errorf("Location = %q, 不該被覆蓋", d.Location)
		}
		if d.Enabled {
			t.Errorf("Enabled = true, 既有設備被停用過就該維持 false")
		}
	})

	t.Run("包裝過的 ErrAlreadyExists 一樣要認得", func(t *testing.T) {
		// store 那層會用 %w 包一層,所以這裡不能用 == 比對。
		repo := &fakeRepo{
			createErr: fmt.Errorf("create device: %w", ErrAlreadyExists),
			getDevice: &Device{Serial: "SN-0001", Name: "既有"},
		}

		d, err := NewService(repo).Register(ctx, validInput)

		assertErrIs(t, err, nil)
		if d.Name != "既有" {
			t.Errorf("Name = %q, 應該走冪等路徑回傳既有那筆", d.Name)
		}
	})

	t.Run("其他錯誤要往上傳,不能吞掉", func(t *testing.T) {
		repo := &fakeRepo{createErr: errDBDown}

		d, err := NewService(repo).Register(ctx, validInput)

		assertErrIs(t, err, errDBDown)
		if d != nil {
			t.Errorf("寫入失敗卻回傳了設備 %+v —— 呼叫端會誤以為註冊成功", d)
		}
	})

	t.Run("撞到已存在但補查失敗,要回錯誤", func(t *testing.T) {
		repo := &fakeRepo{createErr: ErrAlreadyExists, getErr: errDBDown}

		d, err := NewService(repo).Register(ctx, validInput)

		assertErrIs(t, err, errDBDown)
		if d != nil {
			t.Errorf("got %+v, want nil", d)
		}
	})

	t.Run("驗證失敗就不該碰 repository", func(t *testing.T) {
		repo := &fakeRepo{}

		_, err := NewService(repo).Register(ctx, RegisterInput{Serial: "", Name: "溫度計"})

		assertErrIs(t, err, ErrInvalidInput)
		if repo.gotCreate != nil {
			t.Errorf("驗證失敗卻呼叫了 repo.Create")
		}
	})
}

func TestService_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("找到就原樣回傳", func(t *testing.T) {
		repo := &fakeRepo{getDevice: &Device{Serial: "SN-0001", Name: "一樓溫度計"}}

		d, err := NewService(repo).Get(ctx, "SN-0001")

		assertErrIs(t, err, nil)
		if d.Serial != "SN-0001" {
			t.Errorf("Serial = %q, want SN-0001", d.Serial)
		}
	})

	t.Run("找不到時 ErrNotFound 要穿透包裝", func(t *testing.T) {
		// handler 靠 errors.Is 決定回 404,包幾層都必須判斷得到。
		repo := &fakeRepo{}

		_, err := NewService(repo).Get(ctx, "SN-9999")

		assertErrIs(t, err, ErrNotFound)
	})
}

func TestService_List(t *testing.T) {
	ctx := context.Background()

	// 夾值只能從「repo 收到什麼」驗證 —— 回傳值看不出你夾了沒。
	tests := []struct {
		name       string
		limit      int
		offset     int
		wantLimit  int
		wantOffset int
	}{
		{"limit 未指定用預設值", 0, 0, defaultListLimit, 0},
		{"limit 負數也用預設值", -5, 0, defaultListLimit, 0},
		{"limit 超過上限被夾住", 9999, 0, maxListLimit, 0},
		{"limit 剛好等於上限", maxListLimit, 0, maxListLimit, 0},
		{"limit 在範圍內原樣傳下去", 10, 20, 10, 20},
		{"offset 負數夾成 0", 10, -1, 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}

			if _, err := NewService(repo).List(ctx, tt.limit, tt.offset); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if repo.gotLimit != tt.wantLimit {
				t.Errorf("repo 收到 limit = %d, want %d", repo.gotLimit, tt.wantLimit)
			}
			if repo.gotOffset != tt.wantOffset {
				t.Errorf("repo 收到 offset = %d, want %d", repo.gotOffset, tt.wantOffset)
			}
		})
	}

	t.Run("repository 出錯要往上傳", func(t *testing.T) {
		repo := &fakeRepo{listErr: errDBDown}

		_, err := NewService(repo).List(ctx, 10, 0)

		assertErrIs(t, err, errDBDown)
	})
}

func TestService_Update(t *testing.T) {
	ctx := context.Background()

	t.Run("三個欄位都要套上去", func(t *testing.T) {
		repo := &fakeRepo{getDevice: &Device{
			Serial:   "SN-0001",
			Name:     "舊名字",
			Location: "1F",
			Enabled:  true,
		}}
		in := UpdateInput{Name: "新名字", Location: "3F 機房", Enabled: false}

		d, err := NewService(repo).Update(ctx, "SN-0001", in)

		assertErrIs(t, err, nil)
		if d.Name != in.Name {
			t.Errorf("Name = %q, want %q", d.Name, in.Name)
		}
		if d.Location != in.Location {
			t.Errorf("Location = %q, want %q", d.Location, in.Location)
		}
		// Enabled 最容易漏 —— 它是 bool,漏掉時剛好等於「維持原狀」,不會噴錯。
		if d.Enabled != in.Enabled {
			t.Errorf("Enabled = %v, want %v", d.Enabled, in.Enabled)
		}
	})

	t.Run("Serial 不可變", func(t *testing.T) {
		repo := &fakeRepo{getDevice: &Device{Serial: "SN-0001", Name: "舊名字"}}

		d, err := NewService(repo).Update(ctx, "SN-0001", UpdateInput{Name: "新名字"})

		assertErrIs(t, err, nil)
		if d.Serial != "SN-0001" {
			t.Errorf("Serial = %q, 身分不該被更動", d.Serial)
		}
	})

	t.Run("UpdatedAt 要往前走,CreatedAt 不動", func(t *testing.T) {
		created := time.Now().Add(-24 * time.Hour).UTC()
		repo := &fakeRepo{getDevice: &Device{
			Serial:    "SN-0001",
			Name:      "舊名字",
			CreatedAt: created,
			UpdatedAt: created,
		}}

		d, err := NewService(repo).Update(ctx, "SN-0001", UpdateInput{Name: "新名字"})

		assertErrIs(t, err, nil)
		if !d.UpdatedAt.After(created) {
			t.Errorf("UpdatedAt = %v, 應該晚於 %v", d.UpdatedAt, created)
		}
		if !d.CreatedAt.Equal(created) {
			t.Errorf("CreatedAt = %v, 不該被更動", d.CreatedAt)
		}
	})

	t.Run("設備不存在時 ErrNotFound 要穿透", func(t *testing.T) {
		repo := &fakeRepo{}

		_, err := NewService(repo).Update(ctx, "SN-9999", UpdateInput{Name: "新名字"})

		assertErrIs(t, err, ErrNotFound)
		if repo.gotUpdate != nil {
			t.Errorf("設備不存在卻呼叫了 repo.Update")
		}
	})

	t.Run("驗證失敗就不該碰 repository", func(t *testing.T) {
		repo := &fakeRepo{getDevice: &Device{Serial: "SN-0001", Name: "舊名字"}}

		_, err := NewService(repo).Update(ctx, "SN-0001", UpdateInput{Name: ""})

		assertErrIs(t, err, ErrInvalidInput)
		if repo.getCalls != 0 {
			t.Errorf("驗證失敗卻查了 %d 次資料庫", repo.getCalls)
		}
	})

	t.Run("寫入失敗要往上傳", func(t *testing.T) {
		repo := &fakeRepo{
			getDevice: &Device{Serial: "SN-0001", Name: "舊名字"},
			updateErr: errDBDown,
		}

		d, err := NewService(repo).Update(ctx, "SN-0001", UpdateInput{Name: "新名字"})

		assertErrIs(t, err, errDBDown)
		if d != nil {
			t.Errorf("got %+v, want nil", d)
		}
	})
}

func TestService_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("刪掉存在的設備", func(t *testing.T) {
		repo := &fakeRepo{}

		assertErrIs(t, NewService(repo).Delete(ctx, "SN-0001"), nil)
		if repo.gotDelete != "SN-0001" {
			t.Errorf("repo 收到 serial = %q, want SN-0001", repo.gotDelete)
		}
	})

	t.Run("冪等:刪不存在的設備一樣回 nil", func(t *testing.T) {
		// DELETE 影響 0 列在 SQL 裡不是錯誤,所以 repo 回 nil。
		// 客戶端重送 DELETE 時必須一樣拿到成功,而不是 404。
		repo := &fakeRepo{}

		assertErrIs(t, NewService(repo).Delete(ctx, "SN-9999"), nil)
	})

	t.Run("刪不掉時要回錯誤", func(t *testing.T) {
		repo := &fakeRepo{deleteErr: errDBDown}

		assertErrIs(t, NewService(repo).Delete(ctx, "SN-0001"), errDBDown)
	})
}
