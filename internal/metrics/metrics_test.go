package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 這裡的斷言一律對著「一次 scrape 看到什麼」—— series 名稱、label、數值。
// 不去戳 Metrics 的內部欄位:那是實作細節,而 Prometheus 看得到的才是這個
// 套件的對外行為。

// newTestMux 組一個跟 httpapi 同形狀的 mux:有參數化路由,也有固定路由。
func newTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mux.HandleFunc("GET /devices", ok)
	mux.HandleFunc("GET /devices/{serial}", ok)
	mux.HandleFunc("DELETE /devices/{serial}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	return mux
}

// serve 送一個請求進包好 middleware 的 mux。
func serve(m *Metrics, mux http.Handler, method, target string) {
	req := httptest.NewRequest(method, target, nil)
	m.HTTPMiddleware(mux).ServeHTTP(httptest.NewRecorder(), req)
}

// TestHTTPRouteLabelIsThePattern 是這個套件最重要的一條測試。
//
// route label 必須是路由樣板。如果它變成實際路徑,每台設備都會長出自己的
// time series —— 設備一多就是 cardinality 爆炸,而且是安靜地爆。
func TestHTTPRouteLabelIsThePattern(t *testing.T) {
	m := New()
	mux := newTestMux()

	serve(m, mux, http.MethodGet, "/devices/SN-0001")
	serve(m, mux, http.MethodGet, "/devices/SN-0002")
	serve(m, mux, http.MethodGet, "/devices/SN-0003")

	// 三個不同的 serial,應該全部落在同一條 series 上。
	got := testutil.ToFloat64(m.httpRequests.WithLabelValues("GET", "/devices/{serial}", "200"))
	if got != 3 {
		t.Errorf("route=/devices/{serial} count = %v, want 3", got)
	}

	// 而且不該有任何以實際 serial 當 label 的 series。
	if n := testutil.CollectAndCount(m.httpRequests); n != 1 {
		t.Errorf("http_requests_total series count = %d, want 1 —— label 裡混進了實際路徑?", n)
	}
}

func TestHTTPLabels(t *testing.T) {
	tests := []struct {
		name           string
		method, target string
		wantRoute      string
		wantStatus     string
	}{
		{"固定路由", http.MethodGet, "/devices", "/devices", "200"},
		{"參數化路由", http.MethodGet, "/devices/SN-0001", "/devices/{serial}", "200"},
		{"handler 回非 200", http.MethodDelete, "/devices/SN-0001", "/devices/{serial}", "404"},
		{"沒對到路由", http.MethodGet, "/nope", routeUnmatched, "404"},
		{"沒對到路由,亂打的路徑不進 label", http.MethodGet, "/a/b/c/d", routeUnmatched, "404"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			serve(m, newTestMux(), tt.method, tt.target)

			got := testutil.ToFloat64(m.httpRequests.WithLabelValues(tt.method, tt.wantRoute, tt.wantStatus))
			if got != 1 {
				t.Errorf("method=%s route=%s status=%s count = %v, want 1",
					tt.method, tt.wantRoute, tt.wantStatus, got)
			}
			if n := testutil.CollectAndCount(m.httpDuration); n != 1 {
				t.Errorf("http_request_duration_seconds series count = %d, want 1", n)
			}
		})
	}
}

// TestMetricsPathIsNotCounted 確認 scrape 自己不會被算進去。
//
// 沒有這個,每 5 秒一次的抓取會持續墊高請求數,把真實流量的圖洗掉。
func TestMetricsPathIsNotCounted(t *testing.T) {
	m := New()
	mux := http.NewServeMux()
	mux.Handle("GET "+MetricsPath, m.Handler())

	serve(m, mux, http.MethodGet, MetricsPath)

	if n := testutil.CollectAndCount(m.httpRequests); n != 0 {
		t.Errorf("http_requests_total series count = %d, want 0 —— /metrics 不該被自己計入", n)
	}
}

// fakeHandler 是 grpc.UnaryHandler 的假實作,回傳設定好的結果。
func fakeHandler(err error) grpc.UnaryHandler {
	return func(ctx context.Context, req any) (any, error) { return "resp", err }
}

func TestUnaryServerInterceptorCodeLabel(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"成功", nil, "OK"},
		{"設備沒註冊", status.Error(codes.FailedPrecondition, "not registered"), "FailedPrecondition"},
		{"設備已停用", status.Error(codes.PermissionDenied, "disabled"), "PermissionDenied"},
		{"非 gRPC 的錯誤歸為 Unknown", errors.New("boom"), "Unknown"},
	}

	const method = "/telemetry.v1.TelemetryService/SubmitReadings"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			info := &grpc.UnaryServerInfo{FullMethod: method}

			_, err := m.UnaryServerInterceptor(context.Background(), nil, info, fakeHandler(tt.err))
			if !errors.Is(err, tt.err) {
				t.Fatalf("interceptor 吞掉或改寫了 handler 的錯誤: got %v, want %v", err, tt.err)
			}

			got := testutil.ToFloat64(m.grpcServerRequests.WithLabelValues(method, tt.wantCode))
			if got != 1 {
				t.Errorf("method=%s code=%s count = %v, want 1", method, tt.wantCode, got)
			}
		})
	}
}

func TestUnaryClientInterceptor(t *testing.T) {
	const method = "/device.v1.DeviceService/CheckEligibility"

	m := New()
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return nil
	}

	if err := m.UnaryClientInterceptor(context.Background(), method, nil, nil, nil, invoker); err != nil {
		t.Fatalf("UnaryClientInterceptor() = %v, want nil", err)
	}

	if got := testutil.ToFloat64(m.grpcClientRequests.WithLabelValues(method, "OK")); got != 1 {
		t.Errorf("client count = %v, want 1", got)
	}
	// server 端的指標不該被 client 端的呼叫污染 —— 兩者是分開的兩組 series。
	if n := testutil.CollectAndCount(m.grpcServerRequests); n != 0 {
		t.Errorf("grpc_server_requests_total series count = %d, want 0", n)
	}
}

func TestCountReading(t *testing.T) {
	m := New()

	// 直接用 telemetry.Status.String() 會產生的那些值。
	m.CountReading("accepted")
	m.CountReading("accepted")
	m.CountReading("duplicate")
	m.CountReading("clock_out_of_range")
	m.CountReading("invalid")

	want := map[string]float64{
		"accepted":           2,
		"duplicate":          1,
		"clock_out_of_range": 1,
		"invalid":            1,
	}
	for status, w := range want {
		if got := testutil.ToFloat64(m.readings.WithLabelValues(status)); got != w {
			t.Errorf("status=%s count = %v, want %v", status, got, w)
		}
	}
}

// TestHandlerExposesRegisteredMetrics 從最外面驗一次:打過的請求,
// 真的會出現在 /metrics 的輸出裡。
func TestHandlerExposesRegisteredMetrics(t *testing.T) {
	m := New()
	serve(m, newTestMux(), http.MethodGet, "/devices")

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, MetricsPath, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{
		`http_requests_total{method="GET",route="/devices",status="200"} 1`,
		"http_request_duration_seconds_bucket",
		"go_goroutines", // Go collector 有掛上
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics 的輸出少了 %q", want)
		}
	}
}
