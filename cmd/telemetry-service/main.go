// telemetry-service 接收設備上報的讀數,並向 device-service 查詢上報資格。
//
// 它同時是 server(對設備)與 client(對 device-service)——
// 產生的 _grpc.pb.go 裡兩種東西都有,這裡兩種都用上。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	telemetryv1 "github.com/jerryxcy/device-telemetry-go/gen/telemetry/v1"
	"github.com/jerryxcy/device-telemetry-go/internal/deviceclient"
	"github.com/jerryxcy/device-telemetry-go/internal/grpcapi"
	"github.com/jerryxcy/device-telemetry-go/internal/metrics"
	"github.com/jerryxcy/device-telemetry-go/internal/store"
	"github.com/jerryxcy/device-telemetry-go/internal/telemetry"
)

const (
	shutdownTimeout = 10 * time.Second

	// 一批讀數不該無上限。這個值決定設備一次最多能送多少筆,
	// 調它之前先想清楚 InsertBatch 那個隱含交易會變多長。
	maxRecvMsgSize = 4 << 20 // 4 MB
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dsn := env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/telemetry?sslmode=disable")
	grpcAddr := env("GRPC_ADDR", ":9091")
	deviceAddr := env("DEVICE_GRPC_ADDR", "localhost:9090")
	// 這個服務對外只有 gRPC,但指標與探活得走 HTTP —— 純 gRPC 的服務
	// 仍然需要一個 HTTP 的管理面,這是常態而不是將就。
	adminAddr := env("METRICS_ADDR", ":8081")

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// grpc.NewClient 不會馬上連線 —— 它建立的是一個會自己重連的邏輯連線,
	// 第一次 RPC 才真的撥號。所以 device-service 還沒起來也不會擋住啟動,
	// 只是那段期間的 CheckEligibility 會失敗(而失敗代表「問不到答案」,
	// 不會被誤判成「設備沒註冊」)。
	m := metrics.New()

	conn, err := grpc.NewClient(deviceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// 把這個請求的 request ID 一起帶去 device-service,
		// 兩個服務的 log 才串得起來。
		//
		// client 端的指標也在這裡:同一次呼叫,這邊量到的耗時含網路與排隊,
		// device-service 那邊量到的只有它自己處理的時間。兩者的差距,
		// 就是「下游慢」與「中間慢」的分界。
		grpc.WithChainUnaryInterceptor(grpcapi.UnaryRequestIDClient, m.UnaryClientInterceptor),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                2 * time.Minute,
			Timeout:             20 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return fmt.Errorf("dial device-service %s: %w", deviceAddr, err)
	}
	defer conn.Close()

	// 依賴由外往內注入:main 組裝一切,內層只認得 interface。
	repo := store.NewReadingStore(pool)
	checker := deviceclient.New(conn)
	svc := telemetry.NewService(repo, checker)

	grpcSrv := grpc.NewServer(
		// RequestID 必須排在 Logging 之前,後者才讀得到 ID。
		grpc.ChainUnaryInterceptor(grpcapi.UnaryRequestID, grpcapi.UnaryLogging, m.UnaryServerInterceptor),
		grpc.MaxRecvMsgSize(maxRecvMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 10 * time.Second,
			Time:                  2 * time.Minute,
			Timeout:               20 * time.Second,
		}),
	)
	telemetryv1.RegisterTelemetryServiceServer(grpcSrv, grpcapi.NewTelemetryServer(svc, m))
	reflection.Register(grpcSrv)

	// 監聽在 goroutine 之外做,否則位址被占用時 server 只會安靜地沒起來。
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("listen grpc %s: %w", grpcAddr, err)
	}

	// 管理面只有 /metrics 與 /healthz,沒有業務路由,所以不掛
	// RequestID / Logging —— 每 5 秒一次的 scrape 不值得產生 log。
	adminMux := http.NewServeMux()
	adminMux.Handle("GET "+metrics.MetricsPath, m.Handler())
	adminMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	adminSrv := &http.Server{
		Addr:              adminAddr,
		Handler:           adminMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 緩衝 2:兩個 server 都可能寫入。沒有緩衝的話,先關掉的那邊會讓
	// 另一個 goroutine 永遠卡在送出而洩漏。
	errCh := make(chan error, 2)

	go func() {
		slog.Info("telemetry-service listening", "addr", grpcAddr, "device_service", deviceAddr)
		if err := grpcSrv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- fmt.Errorf("grpc: %w", err)
		}
	}()

	go func() {
		slog.Info("admin listening", "addr", adminAddr)
		if err := adminSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("admin http: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// 兩個 server 並行關閉 —— 依序關的話 gRPC 若用掉整份預算,
	// admin 拿到的就是已經過期的 ctx,比照 device-service 的做法。
	var (
		wg       sync.WaitGroup
		adminErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		stopGRPC(shutdownCtx, grpcSrv)
	}()
	go func() {
		defer wg.Done()
		adminErr = adminSrv.Shutdown(shutdownCtx)
	}()
	wg.Wait()

	return adminErr
}

// stopGRPC 優雅關閉 gRPC server。
//
// GracefulStop 會等到所有進行中的 RPC 結束,而且沒有 timeout 參數 ——
// 只要有一個卡住的 RPC,它就永遠不會返回。所以在外面自己包一層期限,
// 逾時改用 Stop 直接中斷連線。
func stopGRPC(ctx context.Context, s *grpc.Server) {
	done := make(chan struct{})
	go func() {
		s.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("grpc graceful stop timed out, forcing")
		s.Stop()
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
