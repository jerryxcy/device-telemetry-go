// Package httpapi 是 handler 層:負責 HTTP 進出與 JSON 編解碼,不放業務邏輯。
package httpapi

import (
	"context"
	"net/http"

	"github.com/jerryxcy/device-telemetry-go/internal/device"
)

// Pinger 讓 /readyz 可以確認 DB 真的活著,而不是只回 200。
type Pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	svc *device.Service
	db  Pinger
}

func NewHandler(svc *device.Service, db Pinger) *Handler {
	return &Handler{svc: svc, db: db}
}

// Routes 已完成。注意 Go 1.22 之後 ServeMux 直接支援 method 與路徑參數,
// 一般 CRUD 完全不需要 gin/chi 這類第三方 router。
//
// 五支標準 CRUD,路徑裡零個動詞。唯一的特例是 DELETE 的語意是退役而非刪除,
// 理由見 docs/adr/0003。
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)

	mux.HandleFunc("POST /devices", h.registerDevice)
	mux.HandleFunc("GET /devices", h.listDevices)
	mux.HandleFunc("GET /devices/{serial}", h.getDevice)
	mux.HandleFunc("PUT /devices/{serial}", h.updateDevice)
	mux.HandleFunc("DELETE /devices/{serial}", h.retireDevice)

	return Chain(mux, RequestID, Logging)
}

// healthz 是 liveness:process 還活著就回 200。已完成。
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz 是 readiness:相依資源就緒才回 200。已完成。
// docker-compose 的 healthcheck 打的是這支,測試才不會在 DB 還沒好時就開跑。
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Error: "database unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
