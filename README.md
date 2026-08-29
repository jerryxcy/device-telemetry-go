# device-telemetry-go

設備遙測平台 — 以 Go 實作的微服務練習專案。

`device-service` 管理設備的身分與服役狀態,`telemetry-service` 接收設備上報的 Reading,
並向 `device-service` 驗證這台設備是否有資格上報。重點不在功能多寡,而在服務間通訊、
錯誤處理,以及重送情境下的寫入冪等性。

領域詞彙見 [CONTEXT.md](./CONTEXT.md),關鍵設計決策見 [docs/adr/](./docs/adr/)。

## 架構

```
                    HTTP :8080
   admin  ────────────────────►  device-service  ──┐
                                        ▲          │
                             gRPC :9090 │          │
                            (查詢上報資格)          ├──►  PostgreSQL
                                        │          │       devices
   device ────────────────────►  telemetry-service ┘       readings
                  gRPC :9091
```

同一個 PostgreSQL 實例、兩張表 —— `readings` 對 `devices` 有外鍵,所以不能拆庫
(見 [ADR-0003](./docs/adr/0003-application-level-delete.md))。

device-service 同時開 HTTP 與 gRPC,兩者共用同一個 `device.Service`:業務邏輯只有
一份,差別只在傳輸協定。位址可用 `HTTP_ADDR` 與 `GRPC_ADDR` 覆寫。

telemetry-service 只開 gRPC。它同時是 server(對設備)與 client(對 device-service),
位址可用 `GRPC_ADDR` 與 `DEVICE_GRPC_ADDR` 覆寫。

## 技術選擇與理由

| 選擇 | 理由 |
|---|---|
| `net/http` (Go 1.22 ServeMux) | 內建已支援 `GET /devices/{serial}` 這類路由,一般 CRUD 不需要 gin/chi |
| `pgx` + 手寫 SQL | 不用 ORM。型別可見、query 可控,效能問題查得出來 |
| 三層分層 handler / service / store | service 層不知道 HTTP 也不知道 SQL,可獨立測試 |
| interface 定義在使用端 | `device.Repository` 定義在 service 旁邊而非 store 裡,由消費者宣告需求 |
| `log/slog` | 標準庫的結構化日誌,不需要第三方 logger |
| gRPC + `buf` | 服務間通訊以 `.proto` 為契約,兩端程式碼從同一份產生。buf 取代 protoc:設定寫在檔案裡而非一長串指令參數,並附帶 lint 與相容性檢查 |

## 領域模型

設備以**出廠序號**作為身分,而非平台產生的 UUID([ADR-0001](./docs/adr/0001-serial-as-natural-key.md))。

`enabled` 只有開與關兩個狀態,所以是 `bool` 而不是 enum —— 非法值在型別上就不存在,
不需要任何驗證函式去擋。除役的設備維持 Disabled 即可,不需要第三種狀態。

刪除設備會連同它的讀數一起刪除,由應用層在交易裡完成而非 `ON DELETE CASCADE`
([ADR-0003](./docs/adr/0003-application-level-delete.md))。

### 冪等性

兩個地方需要冪等,原因都是「設備會重試」:

**註冊** — 同一個 Serial 重複註冊視為同一次,回傳現有那筆,不覆蓋既有欄位。設備重開機
不該把人在平台上改過的名字蓋回出廠預設值。

**讀數** — `readings` 以 `(serial, recorded_at, metric)` 為主鍵,寫入端
`ON CONFLICT DO NOTHING` 直接吸收重送。這要求 `recorded_at` 由**設備的時鐘**提出而非
平台蓋章 —— 否則重送會產生新的時間值,唯一鍵永遠不衝突,去重會靜默失效
([ADR-0002](./docs/adr/0002-device-clock-as-dedup-key.md))。

## API

五支標準 CRUD,路徑裡沒有動詞。

| Method | Path | 說明 |
|---|---|---|
| `GET` | `/healthz` | liveness |
| `GET` | `/readyz` | readiness,會實際 ping 資料庫 |
| `POST` | `/devices` | 註冊設備,**冪等** |
| `GET` | `/devices` | 列出設備(`?limit=&offset=`) |
| `GET` | `/devices/{serial}` | 取得單一設備 |
| `PUT` | `/devices/{serial}` | 更新 `name` / `location` / `enabled` |
| `DELETE` | `/devices/{serial}` | 刪除設備與其讀數,冪等 |

每個動詞都是字面上的意思,路徑裡沒有動詞。

## 開發

需要 Go 1.26+ 與 Docker。

只有要修改 `proto/` 時才需要多裝 buf 與兩個產生器 —— `gen/` 底下的程式碼已經進版控,
單純建置、跑測試或 build image 都不需要它們:

```bash
brew install buf
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

手動打 gRPC 或跑 gRPC 煙霧測試還需要 grpcurl(gRPC 版的 curl):

```bash
brew install grpcurl
```

```bash
make help
```

起資料庫並在本機跑 device-service:

```bash
make run
```

跑單元測試:

```bash
make test
```

跑端到端煙霧測試(服務要先跑起來):

```bash
./scripts/smoke.sh                       # 預設打 http://localhost:8080
./scripts/smoke.sh http://其他位址:8080
```

它打的是真的 HTTP,所以一次驗證 handler → service → store → PostgreSQL 整條鏈 ——
單元測試用假的 Repository,驗不到 SQL 欄位順序、pgx 錯誤碼與交易。全數通過 exit 0,
任何一項失敗 exit 1,腳本只碰自己建立的 `SMOKE-*` 設備。

gRPC 介面有另一支:

```bash
./scripts/smoke-grpc.sh                  # 預設 http://localhost:8080 與 localhost:9090
```

它用 HTTP 擺好設備狀態、用 gRPC 查詢,所以同時驗證了兩個 server 共用同一個
`device.Service`。重點在「設備沒註冊」必須是一個**成功的回應**(答案放在
`Eligibility` 欄位),只有「查詢失敗」才回 gRPC 錯誤碼 —— 弄反的話
telemetry-service 會把資料庫故障誤判成「這台設備沒註冊」而安靜地丟掉讀數。

跨兩個服務的上報流程另有一支(需要 device-service 與 telemetry-service 都在跑):

```bash
./scripts/smoke-telemetry.sh
```

它驗證的是兩層拒絕的分界:整批的前提不成立(未註冊、已停用)回 gRPC 錯誤碼、
連 `results` 都沒有;單筆的問題(時鐘跑掉、資料不合法)則是正常回應,結果放在
對應的 `ReadingResult`,其他筆照常寫入。

改了 `proto/` 之後要重新產生 Go 程式碼:

```bash
make proto
```

`.proto` 是跨服務的契約,欄位編號才是線上格式的識別碼(改欄位名是安全的,重用編號不是)。
送出改動前確認沒有破壞相容性:

```bash
make proto-breaking
```

改了 `migrations/` 之後要重建資料庫(init script 只在 volume 首次建立時執行):

```bash
make db-reset
```

## 進度

**Day 1 — device-service**

- [x] `RegisterInput.Validate()` / `UpdateInput.Validate()`
- [x] `device.Service` 五個方法(`Register` 與 `Delete` 都要冪等)
- [x] `store.DeviceStore` 五個方法(`pgx.ErrNoRows` → `ErrNotFound`,`23505` → `ErrAlreadyExists`)
- [x] `DeviceStore.Delete` 的交易處理 —— 本專案唯一需要 `Begin`/`Commit`/`Rollback` 的地方
- [x] `httpapi` 五個 handler
- [x] `httpapi.Logging` middleware(需要包一層 ResponseWriter 才拿得到 status code)
- [x] `device_test.go` 的 table-driven test 跑綠

**Day 2 — telemetry-service 與服務間 gRPC**

- [x] proto 定義:`device.v1.DeviceService` 與 `telemetry.v1.TelemetryService`
- [x] device-service 開 gRPC 介面:`CheckEligibility` —— 「沒註冊」是一個答案(放在
      `Eligibility` 欄位),只有「查不到答案」才回 gRPC 錯誤碼
- [x] telemetry-service 的 gRPC server:`SubmitReadings` 批次上報,逐筆回結果
- [x] telemetry-service 作為 client 向 device-service 查詢上報資格
- [x] 上報准入:未註冊 / Disabled / 時鐘超出區間,三種可區分的拒絕
- [x] `store.ReadingStore`:`ON CONFLICT DO NOTHING`,並區分「真的寫入」與「被吸收的重送」
      —— 這是 `ACCEPTED` 與 `DUPLICATE` 分得開的前提
- [ ] repository 層整合測試(testcontainers)—— 涵蓋既有的 `DeviceStore`,它目前 0% 覆蓋
- [x] gRPC 的 request ID:用 metadata 跨服務傳遞,對齊 HTTP 那邊的 `X-Request-ID`
