# 以 gRPC 接收上報,換取逐筆的同步結果回饋

Reading 的上報走 gRPC,與服務之間的 CheckEligibility 用同一套契約。設備與
telemetry-service 直接對話,中間沒有 broker。

換到的是**同步的逐筆結果回饋**:SubmitReadings 回傳與 readings 一一對應的 results,
上報端當場就知道哪幾筆寫入了、哪幾筆是重複、哪幾筆因為時鐘跑掉被拒。部分失敗不會
讓整批失敗,上報端自己決定重送哪幾筆。這個好處只對**收到回饋之後有能力做出不同
行為**的上報端成立 —— 跑得動完整協定堆疊、有餘裕自己排隊的那種。

代價是沒有中間人可以吸收落差。上報端的可用性跟 telemetry-service 綁在一起:服務
重啟,連線就斷、上報就失敗。連線數也由服務自己扛。重試與離線緩衝沒有現成的東西
可用,全落在上報端身上。

電池供電、資源受限的現場感測器不符合這個前提:它收到「第 37 筆時鐘超出範圍」也做
不了什麼,卻要為此扛下重試與排隊。那種設備需要的是相反的取捨 —— 中間放一個 broker,
走 MQTT。本專案目前不支援。

## Consequences

- 加 broker 會讓逐筆回饋消失。MQTT 的 PUBACK 只代表 broker 收到,不代表平台接受了;
  CheckEligibility 的拒絕同樣回不到設備。要保留回饋就得另外設計回應通道 —— 那是一塊
  新設計,不是順手多接一條路。
- broker 也會讓 Received At 變模糊:是 broker 收到的時間,還是 telemetry-service 消費
  到的時間?兩者在積壓時差很遠,而 Liveness 正是建立在 Received At 上。
- gRPC 依賴 HTTP/2。會終結並重組 TLS 的企業 proxy 可能把它降級成 HTTP/1.1,連線就
  斷。這是部署環境的風險,不是協定的錯,但選了就得承擔。
- 平台看不見上報端長什麼樣,只看得到 Serial。它是感測器、閘道器還是主機上的 agent,
  對平台沒有差別,因此也不進 CONTEXT.md。
- 目前沒有任何認證:任何人都能用任何 Serial 上報。加 broker 時會被迫面對這件事
  (broker 要設 credential,而 topic 帶著 Serial,需要 ACL),那才是處理設備身分的時機。
