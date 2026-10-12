ALTER TABLE plans
    ADD COLUMN hwid_required BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN trial         BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN visibility    TEXT    NOT NULL DEFAULT 'public';
UPDATE plans SET hwid_required = TRUE WHERE device_limit IS NOT NULL;
ALTER TABLE plans ADD CONSTRAINT plans_device_limit_hwid CHECK (device_limit IS NULL OR hwid_required);
ALTER TABLE plans ADD CONSTRAINT plans_trial_free CHECK (NOT trial OR price_rub = 0);
ALTER TABLE plans ADD CONSTRAINT plans_visibility_check CHECK (visibility IN ('public', 'private'));
CREATE UNIQUE INDEX plans_one_trial_on_sale ON plans ((TRUE)) WHERE trial AND on_sale;

CREATE TABLE trials (
    user_id     BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    plan_id     BIGINT NOT NULL REFERENCES plans(id),
    period_id   BIGINT NOT NULL,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE trial_devices (
    hwid_hash   TEXT PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plan_invites (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    plan_id     BIGINT NOT NULL REFERENCES plans(id),
    max_uses    INT CHECK (max_uses IS NULL OR max_uses > 0),
    uses        INT NOT NULL DEFAULT 0,
    expires_at  TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_plan_invites_plan ON plan_invites (plan_id);

CREATE TABLE plan_access (
    plan_id     BIGINT NOT NULL REFERENCES plans(id),
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source      TEXT   NOT NULL CHECK (source IN ('invite', 'admin')),
    invite_id   BIGINT REFERENCES plan_invites(id),
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plan_id, user_id)
);
CREATE INDEX idx_plan_access_user ON plan_access (user_id);

CREATE TABLE traffic_daily (
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id     BIGINT NOT NULL REFERENCES nodes(id),
    day         DATE   NOT NULL,
    period_id   BIGINT NOT NULL DEFAULT 0,
    bytes_up    BIGINT NOT NULL DEFAULT 0,
    bytes_down  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day, node_id, period_id)
);
CREATE INDEX idx_traffic_daily_day ON traffic_daily (day);
