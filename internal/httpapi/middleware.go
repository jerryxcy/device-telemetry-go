package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// RequestID 是「已完成範例」,照著它的形狀寫下一個 middleware。
//
// 三個值得注意的地方:
//  1. middleware 的型別就是 func(http.Handler) http.Handler,沒有框架魔法
//  2. 值透過 context 往下傳,key 用自訂型別避免碰撞(所以有 ctxKey)
//  3. 回傳的是 http.HandlerFunc,它是個「有 ServeHTTP 方法的函式型別」
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom 從 context 取出 request ID。
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// Logging 記錄每個請求的 method、path、status、耗時。
//
// TODO(day1): 自己實作。
//
// 卡點提示:http.ResponseWriter 沒有讀取 status code 的方法,
// 你需要包一層自己的 struct 嵌入 http.ResponseWriter 並攔截 WriteHeader。
// 這是 Go 裡「embedding + 覆寫方法」的經典練習。
func Logging(next http.Handler) http.Handler {
	return next
}

// Chain 讓 middleware 可以疊起來。已完成。
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
