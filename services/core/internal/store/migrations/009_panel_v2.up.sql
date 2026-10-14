CREATE TABLE telegram_profiles (
    user_id     BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    username    TEXT,
    first_name  TEXT NOT NULL DEFAULT '',
    last_name   TEXT NOT NULL DEFAULT '',
    search      TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE settings (
    id                BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    subs_title        TEXT NOT NULL DEFAULT 'Sylph',
    subs_support_url  TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO settings DEFAULT VALUES;

CREATE INDEX idx_traffic_daily_node_day ON traffic_daily (node_id, day);
