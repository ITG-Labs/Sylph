DROP TRIGGER trg_subscriptions_bump ON subscriptions;
CREATE TRIGGER trg_subscriptions_bump AFTER INSERT OR UPDATE OR DELETE ON subscriptions
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();

DROP INDEX idx_subscriptions_user_period;
DROP INDEX idx_traffic_batches_received;
DROP INDEX idx_traffic_deltas_reported;
ALTER TABLE traffic_deltas DROP COLUMN user_id;

DROP INDEX idx_user_keys_revoked;
DELETE FROM user_keys WHERE revoked_at IS NOT NULL;
ALTER TABLE user_keys DROP COLUMN revoked_at;
