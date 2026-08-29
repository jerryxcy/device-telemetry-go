// Package grpcapi 是 gRPC 的 handler 層,地位等同 httpapi。
//
// 兩者共用同一個 device.Service —— 業務邏輯只有一份,這裡只負責
// 把 domain 的型別與錯誤翻譯成 protobuf 的形狀。
package grpcapi

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	devicev1 "github.com/jerryxcy/device-telemetry-go/gen/device/v1"
	"github.com/jerryxcy/device-telemetry-go/internal/device"
)

// DeviceServer 實作 devicev1.DeviceServiceServer。
type DeviceServer struct {
	// 內嵌產生的 Unimplemented,是 gRPC 的向前相容機制:
	// proto 之後新增 rpc 時,這個型別不會因為少了方法而編譯失敗。
	devicev1.UnimplementedDeviceServiceServer

	svc *device.Service
}

// 編譯期斷言,對照 store.DeviceStore 那邊的寫法。
var _ devicev1.DeviceServiceServer = (*DeviceServer)(nil)

func NewDeviceServer(svc *device.Service) *DeviceServer {
	return &DeviceServer{svc: svc}
}

// CheckEligibility 回答「這台設備現在可不可以上報讀數」。
//
// TODO(day2): 自己實作。
//
// 對應關係:
//
//	svc.Get 回 ErrNotFound   -> ELIGIBILITY_NOT_REGISTERED
//	查到但 d.Enabled == false -> ELIGIBILITY_DISABLED
//	查到且 Enabled            -> ELIGIBILITY_ELIGIBLE
//	其他錯誤(DB 掛了等)      -> gRPC 錯誤碼,不是 Eligibility 值
//
// 最後一條是重點:**「查不到設備」和「查詢失敗」是兩件事**。
// 前者是一個答案,要放進 Eligibility;後者是沒有答案,要回 status.Error
// (codes.Internal),否則 telemetry-service 會把資料庫故障誤判成「這台設備沒註冊」
// 而丟掉讀數。
//
// 提示:req.GetSerial() 比 req.Serial 安全,req 為 nil 時不會 panic。
func (s *DeviceServer) CheckEligibility(
	ctx context.Context,
	req *devicev1.CheckEligibilityRequest,
) (*devicev1.CheckEligibilityResponse, error) {
	serial := req.GetSerial()
	if serial == "" {
		return nil, status.Error(codes.InvalidArgument, "serial is required")
	}
	d, err := s.svc.Get(ctx, serial)
	switch {
	case err == nil:
		eligibility := devicev1.Eligibility_ELIGIBILITY_ELIGIBLE
		if !d.Enabled {
			eligibility = devicev1.Eligibility_ELIGIBILITY_DISABLED
		}
		return &devicev1.CheckEligibilityResponse{Eligibility: eligibility}, nil
	case errors.Is(err, device.ErrNotFound):
		// 「沒註冊」是一個答案,不是查詢失敗 —— 走回傳值那條。
		return &devicev1.CheckEligibilityResponse{
			Eligibility: devicev1.Eligibility_ELIGIBILITY_NOT_REGISTERED,
		}, nil
	default:
		// 查詢本身失敗:我們沒有答案。完整錯誤只進 log,對外給籠統訊息 ——
		// 跟 httpapi.writeError 的 500 分支同一個原則。
		slog.Error("check eligibility", "serial", serial, "err", err)
		return nil, status.Error(codes.Internal, "eligibility lookup failed")
	}
}
