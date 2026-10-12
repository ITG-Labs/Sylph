CREATE TEMP TABLE period_keep AS
SELECT DISTINCT ON (s.user_id) s.user_id, s.id AS keep_id, s.traffic_used_bytes AS used, p.traffic_limit_bytes AS lim
FROM subscriptions s JOIN plans p ON p.id = s.plan_id
WHERE NOT s.canceled AND s.expires_at > now()
ORDER BY s.user_id, (s.starts_at <= now()) DESC, s.starts_at, s.id;

UPDATE subscriptions s SET expires_at = e.last_end
FROM (SELECT user_id, max(expires_at) AS last_end FROM subscriptions
      WHERE NOT canceled AND expires_at > now() GROUP BY user_id) e, period_keep k
WHERE k.user_id = e.user_id AND s.id = k.keep_id;

DELETE FROM subscriptions s USING period_keep k
WHERE s.user_id = k.user_id AND NOT s.canceled AND s.expires_at > now() AND s.id <> k.keep_id;

UPDATE users u SET status = 'expired_traffic'
FROM period_keep k WHERE k.user_id = u.id AND k.lim IS NOT NULL AND k.used >= k.lim;

DROP TABLE period_keep;

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
