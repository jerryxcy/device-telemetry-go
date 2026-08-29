// device-service 提供設備管理的 HTTP API,同時對其他服務開一個 gRPC 介面。
//
// 兩個 server 共用同一個 device.Service:業務邏輯只有一份,差別只在傳輸協定。
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
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	devicev1 "github.com/jerryxcy/device-telemetry-go/gen/device/v1"
	"github.com/jerryxcy/device-telemetry-go/internal/device"
	"github.com/jerryxcy/device-telemetry-go/internal/grpcapi"
	"github.com/jerryxcy/device-telemetry-go/internal/httpapi"
	"github.com/jerryxcy/device-telemetry-go/internal/store"
)

const (
	shutdownTimeout = 10 * time.Second

	// 一次上報的批次不該無上限。gRPC 預設也是 4 MB,這裡明寫出來
	// 是為了讓它成為一個看得見、改得動的旋鈕。
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

// run 回傳 error 而不是直接 os.Exit,這樣 defer 才會執行。
// 這是 Go 裡很常見的 main/run 拆分模式。
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dsn := env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/telemetry?sslmode=disable")
	httpAddr := env("HTTP_ADDR", ":8080")
	grpcAddr := env("GRPC_ADDR", ":9090")

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// 依賴由外往內注入:main 組裝一切,內層只認得 interface。
	repo := store.NewDeviceStore(pool)
	svc := device.NewService(repo)

	httpSrv := &http.Server{
		Addr:              httpAddr,
		Handler:           httpapi.NewHandler(svc, pool).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// http.Server 那邊設了四個 timeout 防慢速連線,gRPC 對應的旋鈕是
	// keepalive:閒置與連線壽命的上限,加上偵測死掉的對端。
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpcapi.UnaryLogging),
		grpc.MaxRecvMsgSize(maxRecvMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			// 強制定期重連,讓 client 端的負載平衡有機會重新選節點。
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 10 * time.Second,
			// 對端沒動靜時主動 ping,20 秒沒回就當它死了。
			Time:    2 * time.Minute,
			Timeout: 20 * time.Second,
		}),
	)
	devicev1.RegisterDeviceServiceServer(grpcSrv, grpcapi.NewDeviceServer(svc))
	// 開 reflection,grpcurl 就不必手動指定 .proto 檔:
	//   grpcurl -plaintext localhost:9090 list
	//   grpcurl -plaintext -d '{"serial":"SN-1"}' localhost:9090 device.v1.DeviceService/CheckEligibility
	// 等同 HTTP 那邊用 curl 的地位。正式環境通常會關掉。
	reflection.Register(grpcSrv)

	// 監聽要在 goroutine 之外做,否則位址被占用時你只會看到 server 安靜地沒起來。
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("listen grpc %s: %w", grpcAddr, err)
	}

	// 緩衝 2:兩個 server 都可能寫入。沒有緩衝的話,先關掉的那邊會讓
	// 另一個 goroutine 永遠卡在送出而洩漏。
	errCh := make(chan error, 2)

	go func() {
		slog.Info("http listening", "addr", httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http: %w", err)
		}
	}()

	go func() {
		slog.Info("grpc listening", "addr", grpcAddr)
		if err := grpcSrv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- fmt.Errorf("grpc: %w", err)
		}
	}()

	// 任一個 server 掛掉就整個收攤 —— 半殘的服務比死掉的服務更難查。
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// 兩個 server 互不相干,並行關閉 —— 依序關的話 gRPC 若用掉整份預算,
	// HTTP 拿到的就是已經過期的 ctx,等於完全沒有優雅關閉的機會。
	var (
		wg      sync.WaitGroup
		httpErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		stopGRPC(shutdownCtx, grpcSrv)
	}()
	go func() {
		defer wg.Done()
		httpErr = httpSrv.Shutdown(shutdownCtx)
	}()
	wg.Wait()

	return httpErr
}

// stopGRPC 優雅關閉 gRPC server。
//
// GracefulStop 會等到所有進行中的 RPC 結束,而且**沒有 timeout 參數** ——
// 只要有一個卡住的 RPC,它就永遠不會返回。所以在外面自己包一層期限,
// 逾時改用 Stop 直接中斷連線。
//
// http.Server.Shutdown 吃 ctx,所以不需要這層處理;這是兩者 API 的差異。
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
