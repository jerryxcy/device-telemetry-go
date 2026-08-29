// Package deviceclient 是 device-service 的 gRPC 客戶端轉接層。
//
// 它把 protobuf 的 Eligibility 列舉翻譯回 domain 的錯誤,讓 telemetry
// 那一層不必認識 protobuf —— 對照 grpcapi 把 domain 翻譯成 protobuf,
// 方向相反、目的相同:協定的型別不越過邊界。
package deviceclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	devicev1 "github.com/jerryxcy/device-telemetry-go/gen/device/v1"
	"github.com/jerryxcy/device-telemetry-go/internal/device"
	"github.com/jerryxcy/device-telemetry-go/internal/telemetry"
)

// Client 實作 telemetry.EligibilityChecker。
type Client struct {
	rpc devicev1.DeviceServiceClient
}

var _ telemetry.EligibilityChecker = (*Client)(nil)

// New 從既有的連線建立 client。連線的生命週期由呼叫端管理。
func New(conn grpc.ClientConnInterface) *Client {
	return &Client{rpc: devicev1.NewDeviceServiceClient(conn)}
}

// CheckEligibility 問 device-service 這台設備能不能上報。
//
// 三種答案對應三種回傳,見 telemetry.EligibilityChecker 的說明。
// 關鍵是「拒絕」與「問不到」必須分開:前者是 device-service 給的答案,
// 後者代表我們不知道,不能當成拒絕。
func (c *Client) CheckEligibility(ctx context.Context, serial string) error {
	resp, err := c.rpc.CheckEligibility(ctx, &devicev1.CheckEligibilityRequest{Serial: serial})
	if err != nil {
		// 連不上、逾時、對方回 Internal —— 全都是「問不到答案」。
		// 這裡刻意不看 gRPC 的 code:任何錯誤都代表沒有答案。
		return fmt.Errorf("check eligibility %s: %w", serial, err)
	}

	switch e := resp.GetEligibility(); e {
	case devicev1.Eligibility_ELIGIBILITY_ELIGIBLE:
		return nil
	case devicev1.Eligibility_ELIGIBILITY_NOT_REGISTERED:
		return device.ErrNotFound
	case devicev1.Eligibility_ELIGIBILITY_DISABLED:
		return device.ErrDisabled
	default:
		// UNSPECIFIED,或未來版本新增而我們還不認得的值。
		//
		// 絕對不能當成「可以上報」—— 那會讓對方新增一種拒絕理由時,
		// 我們變成默默放行。不認得就是不知道。
		return fmt.Errorf("check eligibility %s: 未知的 eligibility %v", serial, e)
	}
}
