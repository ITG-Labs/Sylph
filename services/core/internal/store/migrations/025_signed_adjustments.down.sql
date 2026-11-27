ALTER TABLE period_pool_usage ADD CONSTRAINT period_pool_usage_bonus_bytes_check CHECK (bonus_bytes >= 0);

DROP TRIGGER trg_period_pool_bonus_insert_bump ON period_pool_usage;
CREATE TRIGGER trg_period_pool_bonus_insert_bump AFTER INSERT ON period_pool_usage
FOR EACH ROW WHEN (NEW.bonus_bytes > 0)
EXECUTE FUNCTION bump_nodes_version();

CREATE OR REPLACE VIEW period_pools AS
    SELECT s.id AS period_id, NULL::BIGINT AS pool_id, -1 AS position, s.traffic_used_bytes AS used_bytes, 0::BIGINT AS bonus_bytes,
           NULL::BIGINT AS base_limit_bytes, NULL::BIGINT AS limit_bytes
    FROM subscriptions s
    UNION ALL
    SELECT s.id, pp.id, pp.position, COALESCE(u.used_bytes, 0), COALESCE(u.bonus_bytes, 0),
           pp.traffic_limit_bytes,
           CASE WHEN pp.traffic_limit_bytes IS NOT NULL
                THEN LEAST(pp.traffic_limit_bytes::numeric + COALESCE(u.bonus_bytes, 0), 9223372036854775807)::BIGINT END
    FROM subscriptions s
    JOIN plan_pools pp ON pp.plan_id = s.plan_id
    LEFT JOIN period_pool_usage u ON u.period_id = s.id AND u.pool_id = pp.id;
