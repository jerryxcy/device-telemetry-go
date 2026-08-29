package grpcapi

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jerryxcy/device-telemetry-go/internal/reqid"
)

// UnaryRequestID 沿用上游帶進來的 request ID,沒有就生一個,
// 並把它放進 context 供後續的 interceptor 與 handler 取用。
//
// 對應 httpapi.RequestID,差別只在取值的地方:HTTP 讀 header,
// gRPC 讀 metadata。存進去的位置是同一個(reqid 套件)。
//
// 必須排在 UnaryLogging 之前,否則 log 那行拿到的會是空字串。
func UnaryRequestID(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	var id string
	// metadata 的 key 一律小寫,而且同一個 key 可以有多個值,所以是 []string。
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(reqid.HeaderKey); len(v) > 0 {
			id = v[0]
		}
	}
	if id == "" {
		id = reqid.New()
	}

	// 回送給呼叫端,對應 HTTP 那邊 w.Header().Set(...)。
	// 失敗只代表 header 已經送出去了,不影響請求本身。
	_ = grpc.SetHeader(ctx, metadata.Pairs(reqid.HeaderKey, id))

	return handler(reqid.With(ctx, id), req)
}

// UnaryRequestIDClient 是**客戶端**的 interceptor:把 context 裡的
// request ID 附到送出去的 metadata 上。
//
// 這是跨服務追蹤能成立的那一半 —— 沒有它,下游服務會生一個自己的 ID,
// 兩邊的 log 就串不起來了。
func UnaryRequestIDClient(
	ctx context.Context,
	method string,
	req, reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	if id := reqid.From(ctx); id != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, reqid.HeaderKey, id)
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}

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
		"request_id", reqid.From(ctx),
	)

	return resp, err
}
