//go:build integration

package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testDSN 指向整包整合測試共用的資料庫,由 TestMain 準備好。
var testDSN string

// TestMain 讓整包測試共用一個容器。
//
// 每個測試各起一個容器會正確但太慢(啟動加上 initdb 大約數秒),
// 而這些測試彼此用不同的 serial,不會互相干擾。
func TestMain(m *testing.M) {
	code, err := runIntegrationTests(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "整合測試環境準備失敗:%v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runIntegrationTests(m *testing.M) (int, error) {
	// 明確給了 DATABASE_URL 就用它,不啟容器 —— 開發時對著 make up 起來的
	// 資料庫跑比較快。CI 與預設情況都走容器,才有乾淨且可重現的環境。
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		testDSN = dsn
		return m.Run(), nil
	}

	ctx := context.Background()

	// 用專案實際的 migration 建 schema,而不是在測試裡另寫一份 CREATE TABLE。
	// 這讓這些測試同時也是 migration 的回歸測試:欄位改名而 store 沒跟上,
	// 或 migration 本身寫壞了,這裡都會紅。
	initScript, err := filepath.Abs(filepath.Join("..", "..", "migrations", "001_init.sql"))
	if err != nil {
		return 0, fmt.Errorf("找不到 migration:%w", err)
	}

	ctr, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("telemetry"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.WithInitScripts(initScript),
		// 不是預設值,一定要自己帶上。init script 跑完 postgres 會重啟一次,
		// 所以要等 "ready to accept connections" 出現兩次;第二段等 docker
		// 真的把 port 轉出來 —— 少了它在 macOS / Windows 上會 flaky。
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return 0, fmt.Errorf("啟動 postgres 容器失敗(docker 有在跑嗎?):%w", err)
	}
	// 不管測試成功與否都要收掉容器,所以用 defer 而不是寫在結尾。
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := testcontainers.TerminateContainer(ctr, testcontainers.StopContext(stopCtx)); err != nil {
			fmt.Fprintf(os.Stderr, "收拾容器失敗:%v\n", err)
		}
	}()

	testDSN, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return 0, fmt.Errorf("取得連線字串失敗:%w", err)
	}

	return m.Run(), nil
}

// testPool 開一個連到測試資料庫的連線池,測試結束時關閉。
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := NewPool(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("連不上測試資料庫:%v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
