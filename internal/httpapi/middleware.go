package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jerryxcy/device-telemetry-go/internal/reqid"
)

// statusRecorder 包住 http.ResponseWriter,把 handler 寫出的 status code 記下來。
//
// 用嵌入(embedding)而不是自己存一個欄位:沒有明寫的方法會自動轉發給內層,
// 所以它自動滿足 http.ResponseWriter,只有 WriteHeader 是我們接手的。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(status int) {
	rec.status = status
	rec.ResponseWriter.WriteHeader(status)
}

// RequestID 沿用上游帶進來的 request ID,沒有就生一個。
//
// 三個值得注意的地方:
//  1. middleware 的型別就是 func(http.Handler) http.Handler,沒有框架魔法
//  2. 值透過 context 往下傳,存取都走 reqid 套件 —— 那是這個 process 裡
//     request ID 的唯一存放處,gRPC 那邊的 interceptor 用的是同一個
//  3. 回傳的是 http.HandlerFunc,它是個「有 ServeHTTP 方法的函式型別」
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(reqid.HeaderKey)
		if id == "" {
			id = reqid.New()
		}
		w.Header().Set(reqid.HeaderKey, id)
		next.ServeHTTP(w, r.WithContext(reqid.With(r.Context(), id)))
	})
}

// RequestIDFrom 從 context 取出 request ID。
func RequestIDFrom(ctx context.Context) string {
	return reqid.From(ctx)
}

// Logging 記錄每個請求的 method、path、status、耗時。
//
// status 要靠 statusRecorder 攔下來 —— http.ResponseWriter 只能寫,
// 沒有任何方法可以讀回 handler 送出的 status code。
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// handler 只呼叫 Write 而不呼叫 WriteHeader 時,net/http 會隱含補一個 200
		// 但那不會經過我們的 WriteHeader,所以預設值得自己填。
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", float64(time.Since(start).Microseconds())/1000,
			"request_id", RequestIDFrom(r.Context()),
		)
	})
}

// Chain 讓 middleware 可以疊起來。已完成。
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
