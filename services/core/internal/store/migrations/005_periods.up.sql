ALTER TABLE user_keys ADD COLUMN revoked_at TIMESTAMPTZ;
CREATE INDEX idx_user_keys_revoked ON user_keys (revoked_at) WHERE revoked_at IS NOT NULL;

ALTER TABLE traffic_deltas ADD COLUMN user_id BIGINT;
UPDATE traffic_deltas td SET user_id = uk.user_id FROM user_keys uk WHERE uk.uuid = td.key_uuid;
CREATE INDEX idx_traffic_deltas_reported ON traffic_deltas (reported_at);
CREATE INDEX idx_traffic_batches_received ON traffic_batches (received_at);
CREATE INDEX idx_subscriptions_user_period ON subscriptions (user_id, starts_at) WHERE NOT canceled;

DROP TRIGGER trg_subscriptions_bump ON subscriptions;
CREATE TRIGGER trg_subscriptions_bump
AFTER INSERT OR DELETE OR UPDATE OF user_id, plan_id, starts_at, expires_at, canceled ON subscriptions
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();

UPDATE users SET status = 'active' WHERE status = 'expired_traffic';
