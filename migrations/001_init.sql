-- 由 docker-compose 掛進 postgres 的 /docker-entrypoint-initdb.d/ 自動執行。
-- 注意:只有在 volume 第一次建立時會跑。改了這個檔要 `make db-reset`。

CREATE TABLE IF NOT EXISTS devices (
    -- Serial 由設備自己提出,不是平台產生的。見 docs/adr/0001。
    serial      TEXT        PRIMARY KEY,
    -- Name 只是給人看的標籤,不唯一 —— 兩台設備可以同名。
    name        TEXT        NOT NULL,
    location    TEXT        NOT NULL DEFAULT '',
    -- 平台要不要收這台設備的讀數。兩個狀態,所以是 bool 不是 enum。
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Day 2 才會用到。
CREATE TABLE IF NOT EXISTS readings (
    -- 刻意沒有 ON DELETE CASCADE:刪除由應用層在交易裡負責。見 docs/adr/0003。
    serial      TEXT             NOT NULL REFERENCES devices(serial),
    -- 設備自己的時鐘。這是去重的依據,所以必須由設備提出。見 docs/adr/0002。
    recorded_at TIMESTAMPTZ      NOT NULL,
    metric      TEXT             NOT NULL,
    value       DOUBLE PRECISION NOT NULL,
    -- 平台蓋的章。與 recorded_at 的差距即為回報延遲。
    received_at TIMESTAMPTZ      NOT NULL DEFAULT now(),
    -- 設備重送同一筆讀數時,ON CONFLICT DO NOTHING 直接吸收。
    PRIMARY KEY (serial, recorded_at, metric)
);
