ALTER TABLE settings
    ADD COLUMN notify_traffic BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN notify_expiry  BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN notify_trial   BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE subscriptions ADD COLUMN closed_manually BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_subscriptions_expires ON subscriptions (expires_at);

CREATE TABLE notifications_sent (
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('traffic_80', 'traffic_95', 'traffic_out', 'expiry_3d', 'expiry_1d', 'expiry_1h', 'expired')),
    ref         TEXT NOT NULL,
    period_id   BIGINT,
    paid_until  TIMESTAMPTZ,
    outcome     TEXT NOT NULL CHECK (outcome IN ('sent', 'unreachable', 'failed')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, kind, ref),
    CHECK ((period_id IS NULL) <> (paid_until IS NULL))
);
