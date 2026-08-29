// Package telemetry 是讀數的 domain 層:定義核心型別與業務錯誤。
// 這一層不知道 gRPC,也不知道 SQL。
//
// 詞彙定義見專案根目錄的 CONTEXT.md。
package telemetry

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// 業務錯誤。上層用 errors.Is 判斷,不要用字串比對。
var (
	ErrInvalidReading = errors.New("invalid reading")

	// ErrClockOutOfRange 是「設備的時鐘跑掉了」,不是「這筆資料有問題」。
	// 分開是因為兩者對設備的意義不同:前者要校時,後者要修韌體。
	ErrClockOutOfRange = errors.New("recorded_at out of acceptable range")
)

// 可接受的設備時鐘偏移。見 docs/adr/0002 —— 平台信任設備的時鐘,
// 這兩個值就是「信任到什麼程度」的具體界線。
//
// 往未來寬一點點就好:設備的時鐘再怎麼跑也不該領先平台太多。
// 往過去要寬很多:設備離線期間會把讀數存著,恢復連線後一次補送。
const (
	MaxClockSkewFuture = 5 * time.Minute
	MaxClockAgePast    = 7 * 24 * time.Hour
)

const maxMetricLen = 128

// Reading 是單一 Device 在單一時間點對單一 Metric 的一筆量測值。
//
// 沒有 ReceivedAt —— 那是平台蓋的章,由資料庫的 DEFAULT now() 填。
// 設備不該有機會提出它。
type Reading struct {
	Serial     string
	RecordedAt time.Time
	Metric     string
	Value      float64
}

// WriteResult 說明一筆 Reading 在儲存層實際發生了什麼。
//
// 兩者對設備而言都代表「不用再重送這筆」,分開是為了讓重送率
// 成為可觀測的數字 —— ON CONFLICT DO NOTHING 之後就再也看不出來了。
type WriteResult int

const (
	// WriteInserted 這筆是新的,已寫入。
	WriteInserted WriteResult = iota
	// WriteDuplicate 同一個 (serial, recorded_at, metric) 已存在,直接吸收。
	WriteDuplicate
)

func (r WriteResult) String() string {
	switch r {
	case WriteInserted:
		return "inserted"
	case WriteDuplicate:
		return "duplicate"
	default:
		return fmt.Sprintf("WriteResult(%d)", int(r))
	}
}

// Validate 檢查這筆讀數本身合不合法,不看時鐘。
//
// 所有錯誤都滿足 errors.Is(err, ErrInvalidReading)。
func (r Reading) Validate() error {
	if strings.TrimSpace(r.Metric) == "" {
		return fmt.Errorf("%w: metric is required", ErrInvalidReading)
	}
	if utf8.RuneCountInString(r.Metric) > maxMetricLen {
		return fmt.Errorf("%w: metric exceeds %d characters", ErrInvalidReading, maxMetricLen)
	}
	if r.RecordedAt.IsZero() {
		return fmt.Errorf("%w: recorded_at is required", ErrInvalidReading)
	}
	// DOUBLE PRECISION 存得下 NaN 與 Inf,但那不是量測值。
	// 讓它們進去的話,之後任何聚合查詢都會被污染成 NaN。
	if math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
		return fmt.Errorf("%w: value must be a finite number", ErrInvalidReading)
	}
	return nil
}

// CheckClock 判斷設備提出的時間可不可信,相對於平台的 now。
//
// 跟 Validate 分開,因為兩者的失敗代表不同的事,上報端要能分辨:
// Validate 失敗是資料壞了,CheckClock 失敗是設備該校時。
func (r Reading) CheckClock(now time.Time) error {
	if r.RecordedAt.After(now.Add(MaxClockSkewFuture)) {
		return fmt.Errorf("%w: %s is too far in the future", ErrClockOutOfRange, r.RecordedAt.Format(time.RFC3339))
	}
	if r.RecordedAt.Before(now.Add(-MaxClockAgePast)) {
		return fmt.Errorf("%w: %s is too far in the past", ErrClockOutOfRange, r.RecordedAt.Format(time.RFC3339))
	}
	return nil
}
