package grpcapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	telemetryv1 "github.com/jerryxcy/device-telemetry-go/gen/telemetry/v1"
	"github.com/jerryxcy/device-telemetry-go/internal/device"
	"github.com/jerryxcy/device-telemetry-go/internal/telemetry"
)

// ReadingCounter 是這一層對指標的需求:把逐筆讀數的處理結果記出去。
//
// 定義在使用端,實作在 metrics 套件。計數放在這裡而不是 telemetry 套件,
// 是因為這一層本來就在做「domain 結果 → 對外表示」的翻譯 ——
// 多一個「domain 結果 → metric」是同一個地位的工作,領域層仍然
// 不知道 HTTP、不知道 SQL、也不知道 Prometheus。
type ReadingCounter interface {
	CountReading(status string)
}

// TelemetryServer 實作 telemetryv1.TelemetryServiceServer。
type TelemetryServer struct {
	telemetryv1.UnimplementedTelemetryServiceServer

	svc     *telemetry.Service
	counter ReadingCounter
}

var _ telemetryv1.TelemetryServiceServer = (*TelemetryServer)(nil)

func NewTelemetryServer(svc *telemetry.Service, counter ReadingCounter) *TelemetryServer {
	return &TelemetryServer{svc: svc, counter: counter}
}

// SubmitReadings 收下一批讀數,逐筆回報結果。
//
// 兩種失敗的形狀不同,對應 Service 那邊的兩層拒絕:
//
//	整批的前提不成立(未註冊、已停用)-> gRPC 錯誤碼,沒有 results
//	單筆的問題(時鐘、資料不合法)    -> 正常回應,結果放在對應的 ReadingResult
func (s *TelemetryServer) SubmitReadings(
	ctx context.Context,
	req *telemetryv1.SubmitReadingsRequest,
) (*telemetryv1.SubmitReadingsResponse, error) {
	readings := make([]telemetry.Reading, 0, len(req.GetReadings()))
	for _, r := range req.GetReadings() {
		readings = append(readings, telemetry.Reading{
			RecordedAt: recordedAt(r),
			Metric:     r.GetMetric(),
			Value:      r.GetValue(),
		})
	}

	outcomes, err := s.svc.Submit(ctx, req.GetSerial(), readings)
	if err != nil {
		return nil, submitError(ctx, req.GetSerial(), err)
	}

	results := make([]*telemetryv1.ReadingResult, len(outcomes))
	for i, o := range outcomes {
		// Status.String() 產生的就是 label 的形狀(accepted、duplicate、
		// clock_out_of_range、invalid),不需要另一張對應表。
		s.counter.CountReading(o.Status.String())
		results[i] = &telemetryv1.ReadingResult{
			Index:   int32(i),
			Status:  readingStatus(o.Status),
			Message: errMessage(o.Err),
		}
	}
	return &telemetryv1.SubmitReadingsResponse{Results: results}, nil
}

// recordedAt 把 proto 的 Timestamp 轉成 time.Time,並把「沒送」對應到零值。
//
// 不能直接用 r.GetRecordedAt().AsTime():nil 的 Timestamp 轉出來是
// 1970-01-01(Unix epoch)而不是 time.Time 的零值,IsZero() 會是 false。
// 那樣「忘了帶 recorded_at」會被判成「時鐘太舊」,而不是「資料不合法」。
func recordedAt(r *telemetryv1.Reading) time.Time {
	ts := r.GetRecordedAt()
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

func readingStatus(s telemetry.Status) telemetryv1.ReadingStatus {
	switch s {
	case telemetry.StatusAccepted:
		return telemetryv1.ReadingStatus_READING_STATUS_ACCEPTED
	case telemetry.StatusDuplicate:
		return telemetryv1.ReadingStatus_READING_STATUS_DUPLICATE
	case telemetry.StatusClockOutOfRange:
		return telemetryv1.ReadingStatus_READING_STATUS_CLOCK_OUT_OF_RANGE
	case telemetry.StatusInvalid:
		return telemetryv1.ReadingStatus_READING_STATUS_INVALID
	default:
		// 對應不到的話回 UNSPECIFIED 而不是猜一個 —— 讓接收端看得出有問題。
		return telemetryv1.ReadingStatus_READING_STATUS_UNSPECIFIED
	}
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// submitError 把 domain 錯誤翻譯成 gRPC status,對應 httpapi.writeError 的地位。
func submitError(ctx context.Context, serial string, err error) error {
	switch {
	case errors.Is(err, device.ErrNotFound):
		// 設備要先註冊才能上報 —— 這是前提不成立,不是參數錯誤。
		return status.Error(codes.FailedPrecondition, "device is not registered")
	case errors.Is(err, device.ErrDisabled):
		return status.Error(codes.PermissionDenied, "device is disabled")
	case errors.Is(err, telemetry.ErrInvalidReading):
		// Submit 只在 serial 本身不合法時回這個 —— 逐筆的資料問題走 results。
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		// 未預期的錯誤:完整內容只進 log,對外給籠統訊息,
		// 不要把 SQL 或內部拓樸洩漏出去。
		slog.ErrorContext(ctx, "submit readings", "serial", serial, "err", err)
		return status.Error(codes.Internal, "failed to store readings")
	}
}
