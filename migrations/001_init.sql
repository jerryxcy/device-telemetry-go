-- 由 docker-compose 掛進 postgres 的 /docker-entrypoint-initdb.d/ 自動執行。
-- 注意:只有在 volume 第一次建立時會跑。改了這個檔要 `make db-reset`。

CREATE TABLE IF NOT EXISTS devices (
    id          TEXT        PRIMARY KEY,
    name        TEXT        NOT NULL,
    location    TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL CHECK (status IN ('active', 'inactive')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS devices_name_key ON devices (name);

-- Day 2 才會用到。讀數表。
CREATE TABLE IF NOT EXISTS readings (
    device_id   TEXT        NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    recorded_at TIMESTAMPTZ NOT NULL,
    metric      TEXT        NOT NULL,
    value       DOUBLE PRECISION NOT NULL,
    -- 冪等的關鍵:設備重送同一筆資料時,ON CONFLICT DO NOTHING 直接吃掉
    PRIMARY KEY (device_id, recorded_at, metric)
);
