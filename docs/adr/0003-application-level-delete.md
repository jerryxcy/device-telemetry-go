# 刪除 Device 時在應用層清理 Reading,不使用 ON DELETE CASCADE

刪除一台 Device 會連同它的 Reading 一起刪除。這件事由應用層在單一交易裡完成
(先刪 `readings`,再刪 `devices`),而不是交給外鍵的 `ON DELETE CASCADE`。

理由:cascade 是寫在 schema 裡的隔空取物。只讀 Go 程式碼的人看到一行
`DELETE FROM devices`,不會意識到它同時清掉了這台設備整年的遙測資料。把刪除順序
攤在程式碼裡,行為就跟著程式碼走,也才測得到。

## Consequences

- `readings` 對 `devices` 的外鍵維持 `NO ACTION`。刪除順序寫反會直接撞 FK 錯誤,
  而不是靜默留下孤兒資料。
- 未來若要提供「刪除設備但保留 Telemetry」的選項,必須先移除這個外鍵 —— 設備列
  消失後 `readings.serial` 會指向不存在的列。這是已知的取捨,不是疏漏。
