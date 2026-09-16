# 以 gRPC 接收上報,並假設上報端是網路穩定的 Gateway

Reading 的上報走 gRPC,與服務之間的 CheckEligibility 用同一套契約。這個選擇成立的
前提是**上報端網路穩定、有電力供應** —— 也就是現場的 Gateway 或主機上的 agent,
而不是電池供電、躲在 NAT 後面的感測器本身。感測器以 BLE、ZigBee 這類短距協定連到
Gateway,由 Gateway 代替它們上報;Serial 仍然是感測器的身分,平台不認識 Gateway。

判準是**兩端之間那條線由誰掌握**。線在自己手上,就挑效率最好的:契約寫在 `.proto`
裡,欄位對不上在編譯期就發現,批次上報的 payload 也比 JSON 小 —— 這是 gRPC。線要
穿過客戶網路的中間盒,就得挑穿得過去的:gRPC 依賴 HTTP/2,遇到會降級成 HTTP/1.1 的
TLS 攔截 proxy 就失效,那種場合該退回 HTTP/1.1。線會斷、設備要省電,則該挑能忍受
斷線的 MQTT。

真正的現場感測器落在第三類。本專案目前不支援,只支援第一類。

## Consequences

- 平台看不見 Gateway:上報端只出示 Device 的 Serial,平台分不出是感測器直連還是
  Gateway 代送。這是刻意的 —— Gateway 是部署拓樸,不是領域概念,因此不進 CONTEXT.md。
- 一批 Reading 對應一個 Serial,所以帶著 N 台設備的 Gateway 要送 N 次請求,資格查詢
  也是 N 次。批次省下的是每筆的開銷,不是每台的。
- 瀏覽器無法直接上報,要 grpc-web 加一層 proxy。管理介面因此留在 HTTP。
- 要支援現場感測器,得另開一條 MQTT 入口。冪等寫入已經就緒((Serial, Recorded At,
  Metric) 唯一,見 ADR-0002),MQTT QoS 1 的重送可以直接落在上面,不需要另做去重。
