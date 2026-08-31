# 設計

從 [README 那張架構圖](../README.md#架構)逐層往下展開。詞彙定義見 [CONTEXT.md](../CONTEXT.md);單一決策的背景見 [ADR](./adr/)。

---

## 一、兩個服務,三種角色

```mermaid
flowchart LR
    dev["設備"] -->|"① SubmitReadings"| ts["telemetry-service"]
    ts -->|"② CheckEligibility"| ds["device-service"]
    ts -->|"③ INSERT readings"| pg[("PostgreSQL")]
    ds --> pg
```

telemetry-service **同時是 server 也是 client** —— 對設備是 server,對 device-service 是 client。gRPC 產生的程式碼裡兩種東西都有,這裡兩種都用上。

順序不能顛倒:**先問資格,再寫入**。

### 為什麼一邊 HTTP、一邊 gRPC

**對人的介面用 HTTP。**瀏覽器、`curl`、後台都能直接打,不需要任何額外工具,錯誤訊息也是人看得懂的 JSON。管理設備就是人在做的事。

**服務之間用 gRPC。**契約寫在 `.proto` 裡,兩端的程式碼從同一份產生 —— 欄位對不上在**編譯期**就會發現,不必等到執行時才在 log 裡看到解析失敗。批次上報的訊息也比 JSON 小得多。

device-service 因此**同時開兩個 server**,但業務邏輯只有一份:`internal/device` 那一層不知道自己是被 HTTP 還是 gRPC 呼叫的。加一種協定不必動到它。

這條分界只適用於**業務介面**。指標與探活走的是 HTTP —— Prometheus 是服務,但它抓 `/metrics` 用的是 HTTP,而不是 gRPC。理由跟對人開 HTTP 一樣:那是這類工具的通用語言,換成 gRPC 只會讓所有現成的東西都用不了。telemetry-service 沒有業務用的 HTTP 介面,仍然要為此開一個管理面。

### 為什麼共用一個資料庫

「一個服務一個資料庫」是微服務的常見預設,這裡刻意沒有這樣做。

原因是 `readings.serial` 對 `devices.serial` 建了外鍵。這個外鍵換到兩件事:資料庫層級保證不會出現指向不存在設備的讀數,以及刪除設備時能在單一交易裡把讀數一起清掉。而跨資料庫的外鍵並不存在 —— 要拆庫就得放棄它,改用最終一致性去補。

代價是兩個服務綁在同一個資料庫上。這是已知的取捨,見 [ADR-0003](./adr/0003-application-level-delete.md)。

### 位址都可以覆寫

沒有設定檔,只有環境變數,而且預設值就寫在讀取的那一行:

| 變數 | 預設值 | 讀取處 |
|---|---|---|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/telemetry?sslmode=disable` | [device](../cmd/device-service/main.go#L55) · [telemetry](../cmd/telemetry-service/main.go#L55) |
| `HTTP_ADDR` | `:8080` | [device](../cmd/device-service/main.go#L56) |
| `GRPC_ADDR` | `:9090`(device)/ `:9091`(telemetry) | [device](../cmd/device-service/main.go#L57) · [telemetry](../cmd/telemetry-service/main.go#L56) |
| `DEVICE_GRPC_ADDR` | `localhost:9090` | [telemetry](../cmd/telemetry-service/main.go#L57) |
| `METRICS_ADDR` | `:8081` | [telemetry](../cmd/telemetry-service/main.go#L60) |

`METRICS_ADDR` 只有 telemetry-service 有。device-service 的指標掛在既有的 `HTTP_ADDR` 上,不另外開 port —— 它本來就有一個對外的 HTTP server 了。

---

## 二、每個服務內部分三層

```mermaid
flowchart TB
    subgraph transport["傳輸層 —— 認識協定,不認識業務"]
        httpapi["internal/httpapi<br/>router · device_handler<br/>middleware · response"]
        grpcapi["internal/grpcapi<br/>device_server · telemetry_server<br/>interceptor"]
        deviceclient["internal/deviceclient<br/>client"]
    end

    subgraph domain["領域層 —— 業務邏輯,不認識協定也不認識 SQL"]
        device["internal/device<br/>device · service"]
        telemetry["internal/telemetry<br/>reading · service"]
    end

    subgraph storage["儲存層 —— 唯一知道 SQL 的地方"]
        store["internal/store<br/>postgres · readings · pool"]
    end

    httpapi --> device
    grpcapi --> device
    grpcapi --> telemetry
    deviceclient --> device
    store -.->|"實作 interface"| device
    store -.->|"實作 interface"| telemetry
    deviceclient -.->|"實作 interface"| telemetry
```

| 套件 | 主要型別 | 檔案 |
|---|---|---|
| [`internal/httpapi`](../internal/httpapi/) | `Handler` · `RequestID` · `Logging` | [router.go](../internal/httpapi/router.go) · [device_handler.go](../internal/httpapi/device_handler.go) · [middleware.go](../internal/httpapi/middleware.go) · [response.go](../internal/httpapi/response.go) |
| [`internal/grpcapi`](../internal/grpcapi/) | `DeviceServer` · `TelemetryServer` | [device_server.go](../internal/grpcapi/device_server.go) · [telemetry_server.go](../internal/grpcapi/telemetry_server.go) · [interceptor.go](../internal/grpcapi/interceptor.go) |
| [`internal/deviceclient`](../internal/deviceclient/) | `Client` | [client.go](../internal/deviceclient/client.go) |
| [`internal/device`](../internal/device/) | `Device` · `Service` · `Repository` | [device.go](../internal/device/device.go) · [service.go](../internal/device/service.go) |
| [`internal/telemetry`](../internal/telemetry/) | `Reading` · `Service` · `Repository` · `EligibilityChecker` | [reading.go](../internal/telemetry/reading.go) · [service.go](../internal/telemetry/service.go) |
| [`internal/store`](../internal/store/) | `DeviceStore` · `ReadingStore` | [postgres.go](../internal/store/postgres.go) · [readings.go](../internal/store/readings.go) · [pool.go](../internal/store/pool.go) |

三層的判準很簡單:

- **`internal/device` 和 `internal/telemetry` 的 import 區塊裡沒有 `net/http`、
  沒有 `grpc`、沒有 `pgx`。**這是可以直接檢查的事實,不是願望。
- **`internal/store` 是唯一出現 SQL 字串的地方。**
- 傳輸層負責的只有三件事:**解析請求、呼叫 Service、把結果寫回去**。

### 為什麼實線與虛線的方向相反

實線是 import 方向:`httpapi` import `device`。

虛線是「實作」:`store` 實作了 `device.Repository`,但那個 interface **定義在 `device` 裡**,不是定義在 `store` 裡。

```go
// internal/device/service.go —— 由「使用者」宣告它需要什麼
type Repository interface {
    Create(ctx context.Context, d *Device) error
    GetBySerial(ctx context.Context, serial string) (*Device, error)
    // ...
}
```

```go
// internal/store/postgres.go —— 沒有任何 "implements" 宣告
type DeviceStore struct{ pool *pgxpool.Pool }
```

這是 Go 跟 Java/Spring 最大的思維差異之一:**interface 是隱式滿足的**,`DeviceStore` 剛好有那幾個方法就算數。好處是 `device` 這一層完全不知道 `store` 存在 —— 測試時塞一個假的實作進去即可,不需要資料庫也不需要 mock 框架。

程式碼裡用一行編譯期斷言把這個關係寫下來:

```go
var _ device.Repository = (*DeviceStore)(nil)
```

方法簽名對不上時,錯誤會指在 `store` 這個檔案,而不是等到 `main` 組裝時才爆。

### 依賴由 `main` 注入

沒有 DI 容器。`cmd/*/main.go` 就是唯一的組裝點,由外往內串:

```go
pool := store.NewPool(ctx, dsn)
repo := store.NewDeviceStore(pool)     // 具體實作
svc  := device.NewService(repo)        // 只認得 Repository interface
h    := httpapi.NewHandler(svc, pool)
```

要知道「這個服務依賴什麼」,讀 `main.go` 就夠了。

---

## 三、一個請求的完整路徑

以「設備上報三筆讀數」為例:

```mermaid
sequenceDiagram
    autonumber
    participant D as 設備
    participant TS as telemetry-service
    participant DS as device-service
    participant PG as PostgreSQL

    D->>TS: SubmitReadings(serial, [r1,r2,r3])
    Note over TS: grpcapi 把 proto 轉成 domain 型別

    TS->>DS: CheckEligibility(serial)
    DS->>PG: SELECT ... FROM devices
    PG-->>DS:一列
    DS-->>TS: ELIGIBLE

    Note over TS:逐筆 Validate + CheckClock<br/>被拒的不進批次

    TS->>PG: INSERT ... ON CONFLICT DO NOTHING RETURNING
    PG-->>TS:逐筆:有回傳列 / 沒有
    TS-->>D: results[3]:ACCEPTED / DUPLICATE / 被拒的原因
```

資格查詢是**每批一次**,不是每筆一次 —— 這也是 proto 把 `serial` 放在 request 層級而不是每筆 `Reading` 上的原因。

---

## 四、錯誤:哪些是答案,哪些是失敗

這是整個專案最容易寫錯、後果也最嚴重的一件事。

### 4.1 「查不到」與「查詢失敗」是兩件事

`CheckEligibility` 有三種結果,但它們不在同一個維度上:

```mermaid
flowchart TB
    q["CheckEligibility(serial)"] --> ok["有答案"]
    q --> no["沒有答案"]
    ok --> e1["ELIGIBLE"]
    ok --> e2["NOT_REGISTERED"]
    ok --> e3["DISABLED"]
    no --> err["gRPC codes.Internal<br/>(資料庫掛了、逾時…)"]
```

「這台設備沒註冊」是一個**明確的答案**,要放進回傳值的 `Eligibility` 欄位;「我查不到」才是錯誤,回 `status.Error`。

在 Go 這邊兩者都是 `err != nil`(`svc.Get` 回的都是 error),所以很自然會一起處理掉。**弄反的後果**:device-service 的資料庫故障會被 telemetry-service 誤判成「這台設備沒註冊」,然後安靜地丟掉讀數 —— 而且外表看起來一切正常。

`deviceclient` 的 `default` 分支也守著同一條線:

```go
default:
    // UNSPECIFIED,或未來版本新增而我們還不認得的值。
    // 絕對不能當成「可以上報」—— 那會讓對方新增一種拒絕理由時,我們變成默默放行。
    return fmt.Errorf("未知的 eligibility %v", e)
```

### 4.2 兩層拒絕:整批的前提 vs 單筆的問題

`SubmitReadings` 的拒絕分成兩種,因為**作用範圍不同**:

| 拒絕 | 回應形狀 | 設備該怎麼辦 |
|---|---|---|
| 未註冊 | gRPC `FailedPrecondition`,**沒有 results** | 先去註冊,不要重送 |
| 已停用 | gRPC `PermissionDenied`,同上 | 聯絡管理者 |
| 時鐘超出區間 | 正常回應,該筆 `CLOCK_OUT_OF_RANGE` | 校時後重送那幾筆 |
| 資料不合法 | 正常回應,該筆 `INVALID` | 修正後重送那幾筆 |

前兩者是整批的前提不成立 —— 一筆都不會寫入,回逐筆結果沒有意義。

### 4.3 sentinel error 與 `%w`

domain 層定義 sentinel:

```go
var (
    ErrNotFound      = errors.New("device not found")
    ErrInvalidInput  = errors.New("invalid input")
    ErrAlreadyExists = errors.New("device already exists")
    ErrDisabled      = errors.New("device is disabled")
)
```

每一層往上傳時用 `%w` 包裝、加上脈絡:

```
store:    pgx.ErrNoRows                          → device.ErrNotFound
service:  fmt.Errorf("get device %s: %w", ...)
handler:  errors.Is(err, device.ErrNotFound)     → HTTP 404 / gRPC NotFound
```

**中間包了幾層都不影響 `errors.Is` 的判斷** —— 這就是 domain 層要定義 sentinel 的唯一理由。用 `%v` 取代 `%w` 會讓這條鏈斷掉,而且不會有任何編譯錯誤。

翻譯成協定的地方各只有一處:HTTP 是 `httpapi.writeError`,gRPC 是 `grpcapi` 裡對應的函式。**未預期的錯誤一律記完整內容到 log、只回籠統訊息給對方** —— 不要把 SQL 錯誤洩漏出去。

---

## 五、冪等性

兩個地方需要冪等,原因都是「設備會重試」。

### 5.1 註冊

同一個 Serial 重複註冊視為同一次,回傳現有那筆,**不覆蓋既有欄位**。設備重開機不該把人在平台上改過的名字蓋回出廠預設值。

```mermaid
flowchart TB
    a["repo.Create"] --> b{"撞到 23505?"}
    b -->|否| c["回傳新建的那筆"]
    b -->|是| d["GetBySerial"] --> e["回傳既有那筆"]
```

`23505` 是 PostgreSQL 的 unique_violation。判斷方式是 `errors.As` 取出 `*pgconn.PgError` 再看 `Code` —— 不能用 `errors.Is`,因為 `23505` 和 `23503`(外鍵) 是同一個型別的不同值。

### 5.2 讀數

`readings` 以 `(serial, recorded_at, metric)` 為主鍵,寫入端 `ON CONFLICT DO NOTHING` 直接吸收重送。

這要求 **`recorded_at` 由設備的時鐘提出**而非平台蓋章 —— 否則每次重送都會產生新的時間值,唯一鍵永遠不衝突,去重會**靜默失效**(不報錯,只是資料被灌水)。見 [ADR-0002](./adr/0002-device-clock-as-dedup-key.md)。

代價是平台信任了設備的時鐘,所以另外做兩件事限制損害:超出可接受區間的 `recorded_at` 會被拒絕(未來 5 分鐘、過去 7 天),以及獨立記錄平台的 `received_at`,兩者的差距就是回報延遲。

### 5.3 刪除

刪一台不存在的設備**不是錯誤**,回 204。而且這是「不多做」換來的 —— `DELETE` 影響 0 列在 SQL 裡本來就不是錯誤,只要 store 那層不去檢查 `RowsAffected`,冪等性自動成立。

（對照 `Update`:改不到列代表設備不存在,**要**檢查 `RowsAffected` 並回 `ErrNotFound`。同一個機制,兩種相反的用法。）

---

## 六、兩個容易踩的實作細節

### 6.1 `RETURNING` 才分得出「寫入」與「被吸收」

`ON CONFLICT DO NOTHING` 撞到重複時安靜地什麼都不做,`RowsAffected` 是 0 —— 但那也可能是別的原因。加上 `RETURNING` 之後:**真的插入才有一列回來,被吸收就是零列**(`pgx.ErrNoRows`)。

這是 `ACCEPTED` 與 `DUPLICATE` 分得開的唯一依據。分開回報不影響設備的行為(兩者都代表「不用再重送這筆」),但讓**重送率變成一個可觀測的數字**。

### 6.2 `pgx.Batch` 是一個隱含交易

`SendBatch` 把整批語句一起送出,只有一次來回 —— 但 PostgreSQL 會把它們當成**一個隱含交易**。任一筆撞到真錯誤,整批回滾,**連前面那些已經回報「寫入成功」的也不例外**。

所以 `InsertBatch` 掃完全部結果才判斷:只要有一筆真錯誤就整批回 `error`、不回傳任何逐筆結果,免得謊報。這樣做是安全的,因為寫入冪等 —— 設備重送整批即可。

也因此,**逐筆拒絕的讀數必須在打資料庫之前就濾掉**。一筆 `metric=""` 進到批次裡會撞 NOT NULL 違反,把整批 100 筆一起拖下水。

這兩件事都由整合測試釘住,假物件測不到。

---

## 七、可觀測性

兩種東西:每個請求一行 log,以及可以畫成圖的指標。前者用來查「這一次請求發生什麼事」,後者用來看「整體現在如何」。

### 7.1 結構化 log 與跨服務的 request ID

每個請求(HTTP 與 gRPC 都一樣)產生一行結構化 log:

```json
{"msg":"request","method":"POST","path":"/devices","status":200,"duration_ms":3.4,"request_id":"a1b2c3d4"}
{"msg":"rpc","method":"/device.v1.DeviceService/CheckEligibility","code":"OK","duration_ms":2.5,"request_id":"a1b2c3d4"}
```

`request_id` 跨服務傳遞:上游沒帶就生一個,帶了就沿用,並附在 outgoing metadata / response header 上。所以**同一個請求在兩個服務的 log 裡是同一個 ID**。

實作上 HTTP 是 middleware(`func(http.Handler) http.Handler`)、gRPC 是 interceptor,形狀不同但概念一樣。共用的 context key 放在 `internal/reqid` —— device-service 同時跑兩種協定,兩邊各自定義 key 的話會變成兩個互不相通的「request ID」。

`/metrics` 是例外,不記 log:Prometheus 每 5 秒抓一次,那些不是值得留下的請求,只會把真正的流量洗掉。telemetry-service 的管理面基於同樣的理由,整個就不掛 `RequestID` 與 `Logging`。同一條規則,兩種做法 —— device-service 的 `/metrics` 跟業務路由在同一個 mux 上,只能逐條排除。

### 7.2 指標

`internal/metrics` 是這個 process 裡指標的唯一存放處,地位比照 `internal/reqid`。它持有自己的 registry(不用 client_golang 的 default registry —— 那是全域狀態,測試之間會互相污染),以及三個收集點與一個暴露點:

| 元件 | 掛在哪 |
|---|---|
| HTTP middleware | `httpapi.Routes()` 的 chain |
| gRPC server interceptor | 兩個服務的 `grpc.NewServer` |
| gRPC client interceptor | telemetry-service 的 `grpc.NewClient` |
| `/metrics` handler | device-service 的 `:8080`、telemetry-service 的 `:8081` |

七個指標:

| 名稱 | 型別 | Labels |
|---|---|---|
| `http_requests_total` | Counter | `method`, `route`, `status` |
| `http_request_duration_seconds` | Histogram | `method`, `route` |
| `grpc_server_requests_total` | Counter | `method`, `code` |
| `grpc_server_request_duration_seconds` | Histogram | `method` |
| `grpc_client_requests_total` | Counter | `method`, `code` |
| `grpc_client_request_duration_seconds` | Histogram | `method` |
| `telemetry_readings_total` | Counter | `status` |

前六個是通用的 RED 指標。剩下兩件事是這個平台特有的:

**`telemetry_readings_total{status="duplicate"}`** 讓冪等機制的效果變成看得見的數字。設備重試越積極這個比例越高 —— 而在這之前,這件事只能翻 log 才知道。label 值直接用 `telemetry.Status.String()`(`accepted` / `duplicate` / `clock_out_of_range` / `invalid`),那已經是 label 的形狀,不需要另一張對應表。

**client 與 server 各量一次同一個 RPC**。telemetry-service 量到的 `CheckEligibility` 耗時包含網路與排隊,device-service 量到的只有它自己處理的時間。兩者的差距,就是「下游慢」與「中間慢」的分界 —— 只量一邊做不到這件事。

### 7.3 四個踩得到的坑

前兩個關於 cardinality。**label 的值只要是外部說了算的,就是一個攻擊面** —— Prometheus 不會拒絕新的 series,只會默默地越長越多、越查越慢。

**route label 必須是路由樣板。**用 `r.URL.Path` 的話,`/devices/SN-0001` 與 `/devices/SN-0002` 是兩條不同的 time series,設備一多就是爆炸,而且是安靜地爆:Prometheus 不會抱怨,只會越來越慢、越吃記憶體。所以用 `r.Pattern`(ServeMux 比對出來的樣板),沒對到路由時一律歸為 `unmatched`。

**method label 必須收斂到已知的集合。**`r.Method` 同樣是客戶端說了算的 —— net/http 接受任何合法的 token,所以遠端用亂編的動詞打幾次,就能替我們生出幾條新的 series。跟 route 是同一個問題的另一個入口,只是比較不明顯。九個標準動詞照原樣留著,其餘一律記成 `other`。

後兩個關於「什麼時候記」。

**指標的 middleware 必須是 chain 的最內層。**`r.Pattern` 是 ServeMux 在 `ServeHTTP` 裡比對出樣板後,**就地寫回同一個 `*http.Request`** 的;而 `RequestID` 用 `r.WithContext` 產生的是一份**複本**。排在 `RequestID` 外面的話,拿到的是複本以外那個原始 request,`Pattern` 永遠是空字串 —— 所有流量都會被歸成 `unmatched`,程式不會報錯,只是資料默默是錯的。同理,樣板要在 `next.ServeHTTP` 回來之後才讀得到。

**記錄要用 `defer`,否則 panic 的請求會從指標上消失。**寫在 `next.ServeHTTP` 後面的話,handler 一 panic 就整段跳過 —— 連「發生過一次請求」都不會留下。而 panic 正是最需要被看見的情況。HTTP 那邊用一個 `completed` 旗標分辨:沒走到最後一行就代表在展開 panic,狀態記成 500;gRPC 則是把 code 的初始值設成 `Internal`,正常返回時才覆蓋掉。

### 7.4 領域層不知道 Prometheus

逐筆讀數的計數放在 `grpcapi` 而不是 `telemetry`。那一層本來就在做「domain 結果 → proto 回應」的翻譯,多一個「domain 結果 → metric」是同一個地位的工作。

兩個新的 interface 都定義在使用端,沿用這個專案一貫的做法:`httpapi.Metrics`(要 middleware 與 handler)、`grpcapi.ReadingCounter`(要一個計數方法)。結果是 `httpapi` 與 `grpcapi` 都**沒有 import `internal/metrics`** —— 把它們接起來的只有 `main`。

### 7.5 抓取與呈現

Prometheus 是**主動去抓**(pull):服務只是被動把當下的數字放在 `/metrics` 上,完全不知道 Prometheus 存在。設定在 [deploy/prometheus.yml](../deploy/prometheus.yml),用 `static_configs` 把兩個 target 寫死。

這跟 k8s 沒有關係。k8s 換掉的只有「target 從哪裡發現」這一段(`kubernetes_sd_configs`),抓取方式、指標模型、查詢語言、儲存全部一樣。`static_configs` 之所以夠用,是因為這裡只有兩個位址固定的服務。

Grafana 的 datasource 與 dashboard 都是檔案,在 [deploy/grafana/](../deploy/grafana/)。手點出來的設定存在 Grafana 自己的資料庫裡,不會進版控,換一台機器就沒了。dashboard 開了 `allowUiUpdates`,可以在 UI 裡改著玩,但**檔案永遠是最終權威** —— 檔案一更新就會蓋掉 UI 上的改動。正確的迭代方式是在 UI 裡調到滿意,再匯出 JSON 覆蓋回檔案。

## 八、還沒做的

- **認證授權**:目前任何人都能打任何 API
- **分頁**:`LIMIT/OFFSET` 在大表上會愈翻愈慢,之後要換 keyset pagination
- **`readings` 的查詢 API**:目前只能寫不能讀
- **可設定的時鐘窗口**:未來 5 分鐘 / 過去 7 天現在寫死在程式碼裡
- **`readings` 的分區與保留策略**:資料只會一直長
- **告警**:有指標但沒有規則。Alertmanager、SLO、error budget 都還沒碰
- **分散式追蹤**:request ID 已經跨服務傳遞,那是 tracing 的雛形,但還不是 tracing
- **更細的 histogram bucket**:目前用 `prometheus.DefBuckets`,對本機的次毫秒延遲太粗 —— 跨服務那張圖的兩條線會落在同一個桶裡看不出差距
- **連線池指標**:`pgxpool.Stat()` 的資料還沒接出來
