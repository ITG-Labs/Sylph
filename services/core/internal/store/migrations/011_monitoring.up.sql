CREATE TABLE services (
    cert_cn               TEXT PRIMARY KEY,
    role                  TEXT NOT NULL CHECK (role IN ('bot', 'subs', 'cron', 'panel', 'monitor')),
    version               TEXT NOT NULL DEFAULT '',
    uptime_sec            BIGINT NOT NULL DEFAULT 0,
    cert_not_after        TIMESTAMPTZ,
    telegram_conflict_at  TIMESTAMPTZ,
    last_seen             TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE health_marks (
    name           TEXT PRIMARY KEY CHECK (name IN ('cron_expire', 'yookassa')),
    ok_at          TIMESTAMPTZ,
    failing_since  TIMESTAMPTZ,
    failed_at      TIMESTAMPTZ,
    error          TEXT
);

ALTER TABLE nodes ADD COLUMN cert_not_after TIMESTAMPTZ;
