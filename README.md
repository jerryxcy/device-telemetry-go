# device-telemetry-go

設備遙測平台 — 以 Go 實作的微服務練習專案。

`device-service` 管理設備的身分與服役狀態,`telemetry-service` 接收設備上報的讀數,
並向 `device-service` 驗證這台設備是否有資格上報。重點不在功能多寡,而在服務間通訊、
錯誤處理,以及重送情境下的寫入冪等性。

領域詞彙見 [CONTEXT.md](./CONTEXT.md),關鍵設計決策見 [docs/adr/](./docs/adr/)。

## 架構

```
                  HTTP
   admin  ──────────────►  device-service  ──►  PostgreSQL
                                  ▲
                                  │ gRPC (驗證上報資格)
                                  │
   device ──────────────►  telemetry-service ──►  PostgreSQL
                  gRPC
```

## 技術選擇與理由

| 選擇 | 理由 |
|---|---|
| `net/http` (Go 1.22 ServeMux) | 內建已支援 `GET /devices/{serial}` 這類路由,一般 CRUD 不需要 gin/chi |
| `pgx` + 手寫 SQL | 不用 ORM。型別可見、query 可控,效能問題查得出來 |
| 三層分層 handler / service / store | service 層不知道 HTTP 也不知道 SQL,可獨立測試 |
| interface 定義在使用端 | `device.Repository` 定義在 service 旁邊而非 store 裡,由消費者宣告需求 |
| `log/slog` | 標準庫的結構化日誌,不需要第三方 logger |

## 領域模型

設備以**出廠序號**作為身分,而非平台產生的 UUID([ADR-0001](./docs/adr/0001-serial-as-natural-key.md))。

Lifecycle 三值互斥,「已退役又啟用中」在型別上無法表示:

```
  register
     │
     ▼
  Enabled  ⇄  Disabled        送修、維護、暫時下線 → 這一層
     │           │
     └─────┬─────┘
           ▼
        Retired                永久除役,終點,不可回復
```

平台不刪除設備,只有退役,歷史讀數全數保留([ADR-0003](./docs/adr/0003-retire-instead-of-delete.md))。

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
| `PUT` | `/devices/{serial}` | 更新 `name` / `location` / `lifecycle` |
| `DELETE` | `/devices/{serial}` | **退役**(非刪除),冪等 |

`PUT` 的 `lifecycle` 只接受 `enabled` / `disabled`;要退役請用 `DELETE`。
對已退役的設備做 `PUT` 一律回 `409`。

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

## 進度

**Day 1 — device-service**

- [ ] `Lifecycle.Valid()`
- [ ] `RegisterInput.Validate()` / `UpdateInput.Validate()`
- [ ] `device.Service` 五個方法(`Register` 要冪等,`Retire` 也要)
- [ ] `store.DeviceStore` 四個方法(`pgx.ErrNoRows` → `ErrNotFound`,`23505` → `ErrAlreadyExists`)
- [ ] `httpapi` 五個 handler
- [ ] `httpapi.Logging` middleware(需要包一層 ResponseWriter 才拿得到 status code)
- [ ] `device_test.go` 的 table-driven test 跑綠

**Day 2 — telemetry-service**

- [ ] proto 定義與 gRPC server(批次上報,逐筆回結果)
- [ ] 透過 gRPC 向 device-service 查詢上報資格
- [ ] 上報准入:未註冊 / Disabled / Retired / 時鐘超出區間,四種可區分的拒絕
- [ ] 讀數寫入與冪等處理
- [ ] repository 層整合測試(testcontainers)
