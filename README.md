# device-telemetry-go

設備遙測平台 — 以 Go 實作的微服務練習專案。

`device-service` 管理設備註冊(CRUD),`telemetry-service` 接收設備上報的讀數並向
`device-service` 驗證設備存在。重點不在功能多寡,而在服務間通訊、錯誤處理、
以及重送情境下的寫入冪等性。

## 架構

```
                  HTTP
   client  ──────────────►  device-service  ──►  PostgreSQL
                                  ▲
                                  │ gRPC (驗證設備存在)
                                  │
   device  ──────────────►  telemetry-service ──►  PostgreSQL
                  gRPC
```

## 技術選擇與理由

| 選擇 | 理由 |
|---|---|
| `net/http` (Go 1.22 ServeMux) | 內建已支援 `GET /devices/{id}` 這類路由,一般 CRUD 不需要 gin/chi |
| `pgx` + 手寫 SQL | 不用 ORM。型別可見、query 可控,效能問題查得出來 |
| 三層分層 handler / service / store | service 層不知道 HTTP 也不知道 SQL,可獨立測試 |
| interface 定義在使用端 | `device.Repository` 定義在 service 旁邊而非 store 裡,由消費者宣告需求 |
| `log/slog` | 標準庫的結構化日誌,不需要第三方 logger |

### 冪等性

`readings` 表以 `(device_id, recorded_at, metric)` 為主鍵。設備網路不穩重送同一筆
讀數時,寫入端 `ON CONFLICT DO NOTHING` 直接吸收,不會產生重複資料。

## 開發

需要 Go 1.26+ 與 Docker。

```bash
make help
```

起資料庫並在本機跑 device-service:

```bash
make run
```

跑測試:

```bash
make test
```

改了 `migrations/` 之後要重建資料庫(init script 只在 volume 首次建立時執行):

```bash
make db-reset
```

## API

| Method | Path | 說明 |
|---|---|---|
| GET | `/healthz` | liveness |
| GET | `/readyz` | readiness,會實際 ping 資料庫 |
| POST | `/devices` | 建立設備 |
| GET | `/devices` | 列出設備(`?limit=&offset=`) |
| GET | `/devices/{id}` | 取得單一設備 |
| PUT | `/devices/{id}` | 更新設備 |
| DELETE | `/devices/{id}` | 刪除設備 |

## 進度

**Day 1 — device-service**

- [ ] `device.Device.Validate()`
- [ ] `device.Service` 五個方法
- [ ] `store.DeviceStore` 五個方法(記得把 `pgx.ErrNoRows` 轉成 `device.ErrNotFound`)
- [ ] `httpapi` 五個 handler
- [ ] `httpapi.Logging` middleware(需要包一層 ResponseWriter 才拿得到 status code)
- [ ] `device_test.go` 的 table-driven test 跑綠

**Day 2 — telemetry-service**

- [ ] proto 定義與 gRPC server
- [ ] telemetry-service 透過 gRPC 呼叫 device-service 驗證設備
- [ ] 讀數寫入與冪等處理
- [ ] repository 層整合測試(testcontainers)
