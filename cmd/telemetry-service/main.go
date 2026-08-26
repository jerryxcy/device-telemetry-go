// telemetry-service 接收設備讀數,並向 device-service 驗證設備存在。
//
// Day 2 才動這支。先放這裡是為了讓專案結構一開始就看得出是多服務。
package main

import (
	"log/slog"
	"os"
)

func main() {
	slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.Info("telemetry-service: not implemented yet, see README day 2")
}
