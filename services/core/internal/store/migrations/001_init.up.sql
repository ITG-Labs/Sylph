CREATE TABLE location_groups (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE nodes (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cert_cn         TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    group_id        BIGINT REFERENCES location_groups(id),
    config          JSONB NOT NULL DEFAULT '{}',
    users_version   BIGINT NOT NULL DEFAULT 0,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    last_heartbeat  TIMESTAMPTZ,
    agent_version   TEXT,
    xray_version    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plans (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name                TEXT NOT NULL UNIQUE,
    price_rub           BIGINT NOT NULL,
    period_days         INT NOT NULL,
    traffic_limit_bytes BIGINT,
    location_group_id   BIGINT REFERENCES location_groups(id),
    device_limit        INT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    telegram_id     BIGINT UNIQUE,
    email           TEXT,
    sub_token       TEXT NOT NULL UNIQUE,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_keys (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    uuid        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE subscriptions (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id         BIGINT NOT NULL REFERENCES plans(id),
    starts_at       TIMESTAMPTZ NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    canceled        BOOLEAN NOT NULL DEFAULT FALSE,
    traffic_used_bytes BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payments (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payment_id  TEXT NOT NULL UNIQUE,
    user_id     BIGINT NOT NULL REFERENCES users(id),
    plan_id     BIGINT NOT NULL REFERENCES plans(id),
    amount_rub  BIGINT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE traffic_deltas (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_id     BIGINT NOT NULL REFERENCES nodes(id),
    key_uuid    TEXT NOT NULL,
    bytes_up    BIGINT NOT NULL,
    bytes_down  BIGINT NOT NULL,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE devices (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    user_agent  TEXT NOT NULL DEFAULT '',
    first_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, fingerprint)
);

CREATE INDEX idx_subscriptions_active ON subscriptions (user_id) WHERE NOT canceled;
CREATE INDEX idx_traffic_deltas_node_time ON traffic_deltas (node_id, reported_at DESC);
CREATE INDEX idx_user_keys_user ON user_keys (user_id);
