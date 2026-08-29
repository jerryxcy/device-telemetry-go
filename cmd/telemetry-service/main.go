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
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	telemetryv1 "github.com/jerryxcy/device-telemetry-go/gen/telemetry/v1"
	"github.com/jerryxcy/device-telemetry-go/internal/deviceclient"
	"github.com/jerryxcy/device-telemetry-go/internal/grpcapi"
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

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// grpc.NewClient 不會馬上連線 —— 它建立的是一個會自己重連的邏輯連線,
	// 第一次 RPC 才真的撥號。所以 device-service 還沒起來也不會擋住啟動,
	// 只是那段期間的 CheckEligibility 會失敗(而失敗代表「問不到答案」,
	// 不會被誤判成「設備沒註冊」)。
	conn, err := grpc.NewClient(deviceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
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
		grpc.ChainUnaryInterceptor(grpcapi.UnaryLogging),
		grpc.MaxRecvMsgSize(maxRecvMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 10 * time.Second,
			Time:                  2 * time.Minute,
			Timeout:               20 * time.Second,
		}),
	)
	telemetryv1.RegisterTelemetryServiceServer(grpcSrv, grpcapi.NewTelemetryServer(svc))
	reflection.Register(grpcSrv)

	// 監聽在 goroutine 之外做,否則位址被占用時 server 只會安靜地沒起來。
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("listen grpc %s: %w", grpcAddr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("telemetry-service listening", "addr", grpcAddr, "device_service", deviceAddr)
		if err := grpcSrv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- fmt.Errorf("grpc: %w", err)
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
	stopGRPC(shutdownCtx, grpcSrv)
	return nil
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
