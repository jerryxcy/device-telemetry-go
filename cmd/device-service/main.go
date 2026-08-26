// device-service 提供設備註冊的 CRUD API。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jerryxcy/device-telemetry-go/internal/device"
	"github.com/jerryxcy/device-telemetry-go/internal/httpapi"
	"github.com/jerryxcy/device-telemetry-go/internal/store"
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
	addr := env("HTTP_ADDR", ":8080")

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// 依賴由外往內注入:main 組裝一切,內層只認得 interface。
	repo := store.NewDeviceStore(pool)
	svc := device.NewService(repo)
	h := httpapi.NewHandler(svc, pool)

	srv := &http.Server{
		Addr:              addr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("device-service listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Graceful shutdown:收到訊號後給進行中的請求 10 秒收尾。
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
