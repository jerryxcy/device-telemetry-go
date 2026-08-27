# Device Telemetry

設備遙測平台:實體設備回報量測讀數,平台負責設備的身分與生命週期管理,以及讀數的收集與保存。

## Language

### 設備

**Device**:
一台會回報遙測讀數的實體裝置。以出廠時就存在的 Serial 作為身分,而非平台賦予的識別碼。
_Avoid_: Sensor, Node, Machine

**Serial**:
設備出廠時即持有的識別碼,是 Device 在本系統中的唯一身分。由設備自己提出,平台不產生。
_Avoid_: Device ID, UUID, Identifier

**Name**:
給人看的設備標籤,用於辨識與搜尋。不是身分,因此不要求唯一 —— 兩台設備可以同名。
_Avoid_: Title, Label, Display name

**Registration**:
設備向平台提出自己的 Serial、請求成為已知 Device 的動作。同一個 Serial 重複註冊視為同一次註冊,不會變更平台上既有的資料。
_Avoid_: Enrollment, Onboarding, Provisioning

### 設備狀態

平台區分兩種截然不同的「狀態」:一種由人設定,一種由觀測推導。兩者互不相干,
不可共用同一個詞。

**Lifecycle**:
Device 的服役狀態,由人設定,三個值互斥:Enabled、Disabled、Retired。互斥是刻意
的 —— 「已退役但啟用中」是矛盾的,設計上就不允許被表示。
_Avoid_: Status, State

**Enabled / Disabled**:
Lifecycle 的其中兩個值。Enabled 表示平台願意接收這台設備的 Reading;Disabled 表示
暫時不收,但設備仍在服役 —— 送修、維護、暫時下線都屬於這裡,不是 Retired。
_Avoid_: Active/Inactive, Status

**Liveness**:
觀測狀態。表示平台最近是否還聽得到這台設備,由最後一次 Reading 推導而來,不可由
人設定。與 Lifecycle 無關:一台 Enabled 的設備完全可能失聯。
_Avoid_: Status, State, Active, Online flag

**Retired**:
Lifecycle 的第三個值,表示設備已永久不再服役。這是終點狀態,不可回復 —— 暫時性
的離線請用 Disabled。其歷史 Reading 全數保留:遙測資料的價值長於設備本身。
_Avoid_: Deleted, Removed, Archived

### 讀數

**Reading**:
單一 Device 在單一時間點對單一 Metric 的一筆量測值。
_Avoid_: Telemetry, Data point, Sample, Event

**Metric**:
被量測的量的名稱,例如 `temperature`、`humidity`。平台不維護受控清單,也不記錄單位 —— 這是為了控制專案規模的刻意簡化,不是疏漏。
_Avoid_: Measurement, Field, Sensor type

**Recorded At**:
Device 自己的時鐘在量測發生當下的時間,由設備提出。是判斷兩筆 Reading 是否為同一筆的依據。
_Avoid_: Timestamp, Time, Created at

**Received At**:
平台收到該筆 Reading 的時間,由平台蓋章。與 Recorded At 的差距即為回報延遲。
_Avoid_: Ingested at, Created at
