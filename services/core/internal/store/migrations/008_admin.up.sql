ALTER TABLE users
    ADD COLUMN banned_at  TIMESTAMPTZ,
    ADD COLUMN ban_reason TEXT;
ALTER TABLE users ADD CONSTRAINT users_status_check CHECK (status IN ('active', 'banned'));

ALTER TABLE subscriptions ADD COLUMN traffic_bonus_bytes BIGINT NOT NULL DEFAULT 0 CHECK (traffic_bonus_bytes >= 0);

DROP TRIGGER trg_subscriptions_bump ON subscriptions;
CREATE TRIGGER trg_subscriptions_bump
AFTER INSERT OR DELETE OR UPDATE OF user_id, plan_id, starts_at, expires_at, canceled, traffic_bonus_bytes ON subscriptions
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();

ALTER TABLE payments
    ADD COLUMN period_id   BIGINT REFERENCES subscriptions(id),
    ADD COLUMN note        TEXT,
    ADD COLUMN refunded_at TIMESTAMPTZ;
ALTER TABLE payments DROP CONSTRAINT payments_status_check;
ALTER TABLE payments ADD CONSTRAINT payments_status_check
    CHECK (status IN ('pending', 'succeeded', 'canceled', 'mismatch', 'rejected', 'refunded'));
CREATE INDEX idx_payments_created ON payments (created_at DESC, id DESC);
CREATE INDEX idx_payments_paid ON payments (paid_at) WHERE paid_at IS NOT NULL;

DROP TRIGGER trg_plans_bump ON plans;
CREATE TRIGGER trg_plans_bump
AFTER UPDATE ON plans
FOR EACH ROW
WHEN (OLD.location_group_id IS DISTINCT FROM NEW.location_group_id OR OLD.traffic_limit_bytes IS DISTINCT FROM NEW.traffic_limit_bytes)
EXECUTE FUNCTION bump_nodes_version();

CREATE TABLE admin_audit (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    peer_cn     TEXT NOT NULL,
    operator    TEXT NOT NULL DEFAULT '',
    method      TEXT NOT NULL,
    route       TEXT NOT NULL,
    path        TEXT NOT NULL,
    entity      TEXT NOT NULL,
    entity_id   TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    details     JSONB
);
CREATE INDEX idx_admin_audit_entity ON admin_audit (entity, entity_id, id DESC);
