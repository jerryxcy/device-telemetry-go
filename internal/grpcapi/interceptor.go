package grpcapi

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// UnaryLogging 記錄每個 unary RPC 的 method、gRPC code 與耗時。
//
// 地位等同 httpapi.Logging,但簡單得多:gRPC 的結果是回傳值,
// 直接就拿得到。HTTP 那邊因為 ResponseWriter 只能寫不能讀,
// 才需要 statusRecorder 包一層去攔 WriteHeader。
//
// status.Code(nil) 回傳 codes.OK,所以成功的呼叫也會有一行 log,
// 不需要另外分支。
func UnaryLogging(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	start := time.Now()

	// handler 就是 http middleware 裡 next.ServeHTTP 的位置:
	// 寫在它前面的是請求進來時做的事,後面的是回應出去後做的事。
	resp, err := handler(ctx, req)

	slog.Info("rpc",
		"method", info.FullMethod,
		"code", status.Code(err).String(),
		"duration_ms", float64(time.Since(start).Microseconds())/1000,
	)

	return resp, err
}
