# Device Telemetry

設備遙測平台:實體設備回報量測讀數,平台負責設備的身分管理,以及讀數的收集與保存。

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
設備向平台提出自己的 Serial、請求成為已知 Device 的動作。同一個 Serial 重複註冊視為
同一次註冊,不會變更平台上既有的資料。
_Avoid_: Enrollment, Onboarding, Provisioning

### 設備狀態

平台區分兩種截然不同的「狀態」:一種由人設定,一種由觀測推導。兩者互不相干,
不可共用同一個詞。

**Enabled / Disabled**:
由人設定,表示平台要不要接收這台設備的 Reading。只有兩個值,沒有第三種狀態 ——
設備除役後就一直是 Disabled,不需要另立名目。
_Avoid_: Active/Inactive, Status, Lifecycle, Retired

**Liveness**:
觀測狀態。表示平台最近是否還聽得到這台設備,由最後一次 Reading 推導而來,不可由
人設定。與 Enabled 無關:一台 Enabled 的設備完全可能失聯。
_Avoid_: Status, State, Active, Online flag

### 讀數

**Telemetry**:
設備持續回報量測資料這件事,以及這些資料的總稱。**不可數** —— 一筆一筆的個體是
Reading,不是「一個 telemetry」。
_Avoid_: Metrics, Data

**Reading**:
單一 Device 在單一時間點對單一 Metric 的一筆量測值。Telemetry 的最小單位。
_Avoid_: Data point, Sample, Event

**Metric**:
被量測的量的名稱,例如 `temperature`、`humidity`。平台不維護受控清單,也不記錄單位 ——
這是為了控制專案規模的刻意簡化,不是疏漏。
_Avoid_: Measurement, Field, Sensor type

**Recorded At**:
Device 自己的時鐘在量測發生當下的時間,由設備提出。是判斷兩筆 Reading 是否為同一筆
的依據。
_Avoid_: Timestamp, Time, Created at

**Received At**:
平台收到該筆 Reading 的時間,由平台蓋章。與 Recorded At 的差距即為回報延遲。
_Avoid_: Ingested at, Created at
