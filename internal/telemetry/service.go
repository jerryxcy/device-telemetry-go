package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Repository 是 telemetry service 對儲存層的需求。
//
// 跟 device.Repository 一樣定義在使用端,實作在 store 套件。
type Repository interface {
	// InsertBatch 回傳的 slice 與 readings 等長且順序一致。
	// 任何一筆撞到真錯誤時整批回滾,此時回 error 而非部分結果。
	InsertBatch(ctx context.Context, readings []*Reading) ([]WriteResult, error)
}

// EligibilityChecker 是 telemetry service 對 device-service 的需求。
//
// 三種結果對應三種回傳:
//
//	nil                    可以上報
//	device.ErrNotFound     這個 Serial 沒註冊過        ┐ 明確的拒絕
//	device.ErrDisabled     設備存在,但平台不收它的讀數  ┘
//	其他 error             查不到答案(device-service 掛了、逾時…)
//
// 最後一類必須跟前兩類分開:把「查不到答案」當成「沒註冊」會讓
// device-service 的故障變成安靜地丟掉讀數。
type EligibilityChecker interface {
	CheckEligibility(ctx context.Context, serial string) error
}

// Status 是單一 Reading 在這次上報中的結果。
type Status int

const (
	// StatusAccepted 這筆是新的,已寫入。
	StatusAccepted Status = iota
	// StatusDuplicate 已經存在,直接吸收。對設備而言同樣代表「不用重送」。
	StatusDuplicate
	// StatusClockOutOfRange 設備的時鐘跑掉了,這筆不收。
	StatusClockOutOfRange
	// StatusInvalid 這筆資料本身不合法(metric 為空、value 不是有限數…)。
	StatusInvalid
)

func (s Status) String() string {
	switch s {
	case StatusAccepted:
		return "accepted"
	case StatusDuplicate:
		return "duplicate"
	case StatusClockOutOfRange:
		return "clock_out_of_range"
	case StatusInvalid:
		return "invalid"
	default:
		return fmt.Sprintf("Status(%d)", int(s))
	}
}

// Outcome 是一筆 Reading 的結果與(被拒時的)原因。
// Err 只供人閱讀,程式判斷一律看 Status。
type Outcome struct {
	Status Status
	Err    error
}

// Service 持有上報的業務邏輯。
type Service struct {
	repo        Repository
	eligibility EligibilityChecker
	// now 讓時鐘檢查可測。正式使用時就是 time.Now。
	now func() time.Time
}

func NewService(repo Repository, eligibility EligibilityChecker) *Service {
	return &Service{
		repo:        repo,
		eligibility: eligibility,
		now:         time.Now,
	}
}

// Submit 收下一批來自同一台設備的讀數,逐筆回報結果。
//
// 拒絕分成兩層,因為它們的作用範圍不同:
//
//   - 整批共通的(未註冊、已停用)回 error —— 這批一筆都不會寫入,
//     設備要做的是先去註冊或聯絡管理者,不是重送。
//   - 逐筆的(時鐘跑掉、資料不合法)放進 Outcome —— 其他筆照常寫入,
//     設備自己決定要不要修正後重送那幾筆。
//
// 回傳的 slice 與 readings 等長且順序一致。
func (s *Service) Submit(ctx context.Context, serial string, readings []Reading) ([]Outcome, error) {
	if strings.TrimSpace(serial) == "" {
		return nil, fmt.Errorf("%w: serial is required", ErrInvalidReading)
	}
	if len(readings) == 0 {
		return nil, nil
	}

	// 資格每批查一次,不是每筆一次 —— 這是 Serial 放在批次層級的理由。
	if err := s.eligibility.CheckEligibility(ctx, serial); err != nil {
		return nil, fmt.Errorf("submit %s: %w", serial, err)
	}

	outcomes := make([]Outcome, len(readings))
	toWrite := make([]*Reading, 0, len(readings))
	// srcIndex[k] 是 toWrite[k] 在原本 readings 裡的位置,
	// 寫入結果回來時要靠它填回正確的格子。
	srcIndex := make([]int, 0, len(readings))

	now := s.now()
	for i := range readings {
		r := readings[i]
		// Serial 由批次決定,不採信逐筆帶的值 —— 一批就是一台設備。
		r.Serial = serial

		if err := r.Validate(); err != nil {
			outcomes[i] = Outcome{Status: StatusInvalid, Err: err}
			continue
		}
		if err := r.CheckClock(now); err != nil {
			outcomes[i] = Outcome{Status: StatusClockOutOfRange, Err: err}
			continue
		}

		toWrite = append(toWrite, &r)
		srcIndex = append(srcIndex, i)
	}

	// 全部被逐筆拒絕的話沒有東西要寫,不必打資料庫。
	if len(toWrite) == 0 {
		return outcomes, nil
	}

	results, err := s.repo.InsertBatch(ctx, toWrite)
	if err != nil {
		return nil, fmt.Errorf("submit %s: %w", serial, err)
	}
	if len(results) != len(toWrite) {
		return nil, fmt.Errorf("submit %s: repository 回了 %d 筆結果,送出 %d 筆", serial, len(results), len(toWrite))
	}

	for k, res := range results {
		switch res {
		case WriteInserted:
			outcomes[srcIndex[k]] = Outcome{Status: StatusAccepted}
		case WriteDuplicate:
			outcomes[srcIndex[k]] = Outcome{Status: StatusDuplicate}
		default:
			return nil, fmt.Errorf("submit %s: 未知的寫入結果 %v", serial, res)
		}
	}

	return outcomes, nil
}
