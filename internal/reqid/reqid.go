// Package reqid 是 request ID 在 context 裡的唯一存放處。
//
// 抽出來是因為 device-service 同時跑 HTTP 與 gRPC,兩邊各自定義
// context key 的話會變成兩個互不相通的「request ID」—— 同一個 process
// 裡有兩種答案,誰都說不清哪個才算數。
//
// 這一層刻意不認識 http 也不認識 grpc:兩種傳輸協定各自負責從自己的
// header / metadata 取值,取到之後都存進這裡。
package reqid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// HeaderKey 是兩種協定共用的欄位名。
//
// HTTP header 不分大小寫,gRPC metadata 一律小寫 —— 用全小寫寫死,
// 兩邊都不會出錯。
const HeaderKey = "x-request-id"

// ctxKey 用未匯出的空結構當 key。
//
// 空結構佔 0 bytes,而且別的套件無法構造出同一個型別的值,
// 所以不可能跟其他人存進 context 的 key 碰撞。
type ctxKey struct{}

// New 產生一個新的 request ID。
//
// 用 crypto/rand 不是因為需要密碼學強度,而是因為它在 Go 1.24 之後
// 保證不回傳錯誤(失敗直接 panic),不必煩惱 seed 或錯誤處理。
func New() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// With 回傳帶著 request ID 的衍生 context。
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From 取出 request ID,沒有就回空字串。
//
// 用雙值型別斷言:單值形式在失敗時會 panic,呼叫端不該為了取一個
// log 欄位而需要處理錯誤。
func From(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}
