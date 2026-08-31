// Package metrics 是這個 process 裡 Prometheus 指標的唯一存放處。
//
// 它同時提供收集與暴露兩件事:HTTP middleware、gRPC 的 server 與 client
// interceptor 負責收集,Handler 負責讓 Prometheus 抓走。
//
// 地位比照 reqid —— 橫切關注點集中在一個套件裡,其他套件只負責接線。
// 領域層(device、telemetry)完全不知道它的存在。
package metrics

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MetricsPath 是暴露指標的路徑。它自己不計入 HTTP 指標 ——
// 否則每 5 秒一次的 scrape 會持續墊高請求數,把真實流量的圖洗掉。
const MetricsPath = "/metrics"

// routeUnmatched 是沒對到任何路由時的 route label。
//
// 不能拿實際路徑當 label:那等於讓任何人用亂打網址的方式,
// 一個請求換我們一條新的 time series。
const routeUnmatched = "unmatched"

// Metrics 持有一份自己的 registry 與所有 collector。
//
// 不用 client_golang 的 default registry:那是全域狀態,測試之間會互相污染,
// 而且註冊了什麼要翻遍整個程式才知道。這裡 New 一次就看得完。
type Metrics struct {
	registry *prometheus.Registry

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec

	grpcServerRequests *prometheus.CounterVec
	grpcServerDuration *prometheus.HistogramVec

	grpcClientRequests *prometheus.CounterVec
	grpcClientDuration *prometheus.HistogramVec

	readings *prometheus.CounterVec
}

// New 建立一組指標並完成註冊。
//
// 註冊完之後 /metrics 的輸出長這樣 —— 每個指標前面都有兩行中繼資料,
// 那是 client_golang 依 Help 與型別自動產生的:
//
//	# HELP http_request_duration_seconds HTTP 請求耗時(秒)。
//	# TYPE http_request_duration_seconds histogram
//	http_request_duration_seconds_bucket{...} 85
//
// Prometheus 抓走之後,le 只是另一個普通的 label,它並不知道這是
// histogram —— 是查詢時的 histogram_quantile 才把那些桶當桶用。
//
// Go 與 process 兩個 collector 由 client_golang 提供,給的是 goroutine 數、
// 記憶體、GC、開檔數這些「服務本身」的狀態 —— 跟業務指標互補。
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),

		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP 請求總數。",
		}, []string{"method", "route", "status"}),

		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP 請求耗時(秒)。",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),

		grpcServerRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "grpc_server_requests_total",
			Help: "本服務處理的 gRPC 請求總數。",
		}, []string{"method", "code"}),

		grpcServerDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "grpc_server_request_duration_seconds",
			Help:    "本服務處理 gRPC 請求的耗時(秒)。",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),

		grpcClientRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "grpc_client_requests_total",
			Help: "本服務發出的 gRPC 請求總數。",
		}, []string{"method", "code"}),

		grpcClientDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "grpc_client_request_duration_seconds",
			Help:    "本服務發出 gRPC 請求的耗時(秒),從呼叫端看到的。",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),

		readings: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "telemetry_readings_total",
			Help: "逐筆讀數的處理結果總數。",
		}, []string{"status"}),
	}

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.httpRequests,
		m.httpDuration,
		m.grpcServerRequests,
		m.grpcServerDuration,
		m.grpcClientRequests,
		m.grpcClientDuration,
		m.readings,
	)

	return m
}

// Handler 回傳 Prometheus 抓取用的 handler。
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Registry 給測試用,讓斷言可以對著「一次 scrape 看到什麼」做。
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// HTTPMiddleware 記錄每個請求的次數與耗時。
//
// 一個請求走過這裡,會在 /metrics 上變成這樣(真的抓下來的):
//
//	http_requests_total{method="POST",route="/devices",status="200"} 86
//	http_requests_total{method="GET",route="/devices",status="400"} 11
//	http_requests_total{method="PATCH",route="unmatched",status="405"} 9
//
//	http_request_duration_seconds_bucket{method="POST",route="/devices",le="0.005"} 85
//	http_request_duration_seconds_bucket{method="POST",route="/devices",le="0.01"}  85
//	  ...(中間十個桶,數字都一樣)
//	http_request_duration_seconds_bucket{method="POST",route="/devices",le="+Inf"} 86
//	http_request_duration_seconds_sum{method="POST",route="/devices"}   0.06887658
//	http_request_duration_seconds_count{method="POST",route="/devices"} 86
//
// counter 那行很直觀:這個組合出現過 86 次。histogram 比較不直觀 ——
// 它不存個別耗時,只存「≤ 某個秒數的有幾筆」,而且是**累計**的,所以
// le 越大數字越大,+Inf 那個就是總數。上面 85 對 86 的差,是有一筆超過
// 10 秒?不是 —— 是有一筆落在 0.005 到 +Inf 之間的某個桶裡。
//
// 一個 histogram 因此展開成 14 條 series(12 個桶 + sum + count),
// 這也是為什麼 histogram 的 label 特別貴。
//
// route label 用的是**路由樣板**而不是實際路徑 —— /devices/SN-0001 與
// /devices/SN-0002 必須算同一條 series,否則設備一多就是 cardinality 爆炸。
//
// 樣板要在 next.ServeHTTP **回來之後**才讀得到:ServeMux 是在 ServeHTTP
// 裡比對出樣板後,就地寫回同一個 *http.Request 的 Pattern 欄位的。
// 進來的當下那個欄位還是空的。
func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == MetricsPath {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		// 用 defer 記錄,handler panic 時這筆才不會整個消失 ——
		// panic 正是最需要被看見的情況。completed 用來分辨這兩條路:
		// 沒有走到最後一行就代表在展開 panic,狀態記成 500。
		completed := false
		defer func() {
			status := rec.status
			if !completed {
				status = http.StatusInternalServerError
			}
			route := routeLabel(r.Pattern)
			method := methodLabel(r.Method)
			m.httpRequests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
			m.httpDuration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
		}()

		next.ServeHTTP(rec, r)
		completed = true
	})
}

// methodLabel 把 HTTP method 收斂到已知的集合。
//
// r.Method 是客戶端送什麼就是什麼 —— net/http 接受任何合法的 token,
// 所以不收斂的話,遠端用亂編的動詞打幾次就能替我們生出幾條新的 series。
// 這跟 route 用實際路徑是同一種 cardinality 問題,只是入口不同。
func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "other"
	}
}

// routeLabel 把 ServeMux 的樣板整理成 label 值。
//
// 樣板的格式是 "[METHOD ][HOST]/[PATH]",例如 "GET /devices/{serial}"。
// method 已經是獨立的 label,這裡把它去掉,只留路徑的部分。
func routeLabel(pattern string) string {
	if pattern == "" {
		return routeUnmatched
	}
	if i := strings.LastIndex(pattern, " "); i >= 0 {
		return pattern[i+1:]
	}
	return pattern
}

// statusRecorder 攔下 handler 寫出的 status code。
//
// http.ResponseWriter 只能寫不能讀,拿不回已送出的 status,只好包一層。
// httpapi 的 Logging 也有一個同樣的東西 —— 兩份是刻意的:
// 這樣 metrics 套件不必依賴 httpapi,兩邊可以各自獨立使用。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(status int) {
	rec.status = status
	rec.ResponseWriter.WriteHeader(status)
}

// UnaryServerInterceptor 記錄本服務處理的每個 unary RPC。
//
// telemetry-service 上抓下來的實際內容:
//
//	grpc_server_requests_total{code="OK",method="/telemetry.v1.TelemetryService/SubmitReadings"}                 45
//	grpc_server_requests_total{code="FailedPrecondition",method="/telemetry.v1.TelemetryService/SubmitReadings"} 18
//	grpc_server_requests_total{code="PermissionDenied",method="/telemetry.v1.TelemetryService/SubmitReadings"}    9
//	grpc_server_requests_total{code="InvalidArgument",method="/telemetry.v1.TelemetryService/SubmitReadings"}     9
//
// 這四個 code 對得上 submitError 的四個分支:沒註冊、已停用、serial 不合法、
// 其餘正常。注意 OK 的 45 筆裡面仍然可能有逐筆被拒的讀數 —— 那是整批成功、
// 個別失敗,計數在 CountReading 那邊。
//
// 對應 HTTP 那邊的 HTTPMiddleware,但簡單得多:gRPC 的結果是回傳值,
// 直接就拿得到,不需要 statusRecorder。
func (m *Metrics) UnaryServerInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	start := time.Now()

	// handler panic 時 code 就停在這個初始值 —— 跟 HTTP 那邊一樣,
	// 不能讓 panic 的請求從指標上消失。
	code := codes.Internal.String()
	defer func() {
		m.grpcServerRequests.WithLabelValues(info.FullMethod, code).Inc()
		m.grpcServerDuration.WithLabelValues(info.FullMethod).Observe(time.Since(start).Seconds())
	}()

	resp, err := handler(ctx, req)
	// status.Code(nil) 是 codes.OK,成功的呼叫不需要另外分支。
	code = status.Code(err).String()

	return resp, err
}

// UnaryClientInterceptor 記錄本服務**發出**的每個 unary RPC。
//
// 同一次呼叫在兩個服務上各留下一筆,名字不同:
//
//	telemetry-service :8081/metrics
//	  grpc_client_requests_total{code="OK",method="/device.v1.DeviceService/CheckEligibility"} 63
//
//	device-service :8080/metrics
//	  grpc_server_requests_total{code="OK",method="/device.v1.DeviceService/CheckEligibility"} 63
//
// 次數一樣(沒掉封包),但耗時不會一樣 —— 這正是重點,見下面。
//
// 這是跨服務問題能分辨的那一半:同一次呼叫,呼叫端量到的耗時包含網路與排隊,
// 被呼叫端量到的只有自己處理的時間。兩者的差距就是「下游慢」與「自己慢」的分界。
func (m *Metrics) UnaryClientInterceptor(
	ctx context.Context,
	method string,
	req, reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	start := time.Now()

	code := codes.Internal.String()
	defer func() {
		m.grpcClientRequests.WithLabelValues(method, code).Inc()
		m.grpcClientDuration.WithLabelValues(method).Observe(time.Since(start).Seconds())
	}()

	err := invoker(ctx, method, req, reply, cc, opts...)
	code = status.Code(err).String()

	return err
}

// CountReading 累加一筆讀數的處理結果。
//
// 跑一輪 smoke test 之後 telemetry-service 上長這樣:
//
//	telemetry_readings_total{status="accepted"}           36
//	telemetry_readings_total{status="duplicate"}          27
//	telemetry_readings_total{status="clock_out_of_range"} 18
//	telemetry_readings_total{status="invalid"}            18
//
// duplicate 佔 27/99 ≈ 27% —— 那是冪等寫入實際擋下的重送。這個數字
// 本來只能翻 log 一筆一筆數,現在是一條可以畫成圖的線。
//
// status 直接吃 telemetry.Status.String() 的輸出(accepted、duplicate、
// clock_out_of_range、invalid)—— 那已經是 label 的形狀,不需要另一張對應表。
func (m *Metrics) CountReading(status string) {
	m.readings.WithLabelValues(status).Inc()
}
