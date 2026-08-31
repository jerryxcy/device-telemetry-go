// Package httpapi 是 handler 層:負責 HTTP 進出與 JSON 編解碼,不放業務邏輯。
package httpapi

import (
	"context"
	"net/http"

	"github.com/jerryxcy/device-telemetry-go/internal/device"
)

// metricsPath 上的請求不記 log。每 5 秒一次的 scrape 不值得產生一行,
// 那只會把真正的請求洗掉。telemetry-service 的管理面基於同樣的理由,
// 整個就不掛 RequestID 與 Logging。
const metricsPath = "/metrics"

// Pinger 讓 /readyz 可以確認 DB 真的活著,而不是只回 200。
type Pinger interface {
	Ping(ctx context.Context) error
}

// Metrics 是 handler 層對指標的需求:一個收集用的 middleware,
// 加一個給 Prometheus 抓取的 handler。
//
// 跟 Pinger 一樣定義在使用端 —— httpapi 因此完全不必 import metrics 套件。
type Metrics interface {
	HTTPMiddleware(next http.Handler) http.Handler
	Handler() http.Handler
}

type Handler struct {
	svc     *device.Service
	db      Pinger
	metrics Metrics
}

func NewHandler(svc *device.Service, db Pinger, metrics Metrics) *Handler {
	return &Handler{svc: svc, db: db, metrics: metrics}
}

// Routes 已完成。注意 Go 1.22 之後 ServeMux 直接支援 method 與路徑參數,
// 一般 CRUD 完全不需要 gin/chi 這類第三方 router。
//
// 五支標準 CRUD,路徑裡零個動詞,每個動詞都是字面上的意思。
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.Handle("GET "+metricsPath, h.metrics.Handler())

	mux.HandleFunc("POST /devices", h.registerDevice)
	mux.HandleFunc("GET /devices", h.listDevices)
	mux.HandleFunc("GET /devices/{serial}", h.getDevice)
	mux.HandleFunc("PUT /devices/{serial}", h.updateDevice)
	mux.HandleFunc("DELETE /devices/{serial}", h.deleteDevice)

	// 指標的 middleware 必須是最內層,直接包住 mux。
	//
	// 它要讀的 r.Pattern 是 ServeMux 在比對出路由後,就地寫回那個
	// *http.Request 的。而 RequestID 用 r.WithContext 產生的是一份**複本** ——
	// 排在它外面的話,拿到的會是複本以外的那個原始 request,Pattern 永遠是空的。
	return Chain(mux, RequestID, Logging, h.metrics.HTTPMiddleware)
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
