package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jerryxcy/device-telemetry-go/internal/device"
)

type errorBody struct {
	Error string `json:"error"`
}

// writeJSON 幫你寫好,純樣板。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "err", err)
	}
}

// writeError 把 domain 錯誤翻譯成 HTTP status。
//
// 這是「已完成範例」:注意 errors.Is 的用法 —— 即使 service 層用 %w 包了好幾層,
// 這裡依然判斷得到。這就是為什麼 domain 層要定義 sentinel error。
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, device.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Error: err.Error()})
	case errors.Is(err, device.ErrAlreadyExists):
		writeJSON(w, http.StatusConflict, errorBody{Error: err.Error()})
	case errors.Is(err, device.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
	default:
		// 未預期的錯誤:記完整內容到 log,但只回籠統訊息給 client,
		// 不要把 SQL 錯誤洩漏出去。
		slog.Error("unhandled error", "err", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal error"})
	}
}
