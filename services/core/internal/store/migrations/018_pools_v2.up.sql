CREATE TEMP TABLE pv2_cover AS
    SELECT plan_id, inbound_id, node_id, pool_id FROM plan_inbounds;

CREATE TEMP TABLE pv2_free_nodes AS
    SELECT n.id AS node_id FROM nodes n
    WHERE NOT EXISTS (SELECT 1 FROM group_nodes gn WHERE gn.node_id = n.id)
      AND NOT EXISTS (SELECT 1 FROM group_inbounds gi JOIN node_inbounds i ON i.id = gi.inbound_id WHERE i.node_id = n.id);

CREATE TEMP TABLE pv2_free_inbounds AS
    SELECT i.id AS inbound_id FROM node_inbounds i
    WHERE NOT EXISTS (SELECT 1 FROM inbound_memberships m WHERE m.inbound_id = i.id)
      AND NOT EXISTS (SELECT 1 FROM pv2_free_nodes f WHERE f.node_id = i.node_id);

INSERT INTO group_nodes (group_id, node_id)
SELECT DISTINCT p.location_group_id, f.node_id FROM plans p CROSS JOIN pv2_free_nodes f
WHERE p.location_group_id IS NOT NULL
ON CONFLICT DO NOTHING;

INSERT INTO group_inbounds (group_id, inbound_id)
SELECT DISTINCT p.location_group_id, f.inbound_id FROM plans p CROSS JOIN pv2_free_inbounds f
WHERE p.location_group_id IS NOT NULL
ON CONFLICT DO NOTHING;

DO $$
DECLARE
    gid BIGINT;
    nm  TEXT := 'Общие входы';
    k   INT  := 1;
BEGIN
    IF EXISTS (SELECT 1 FROM plans WHERE location_group_id IS NULL)
       AND (EXISTS (SELECT 1 FROM pv2_free_nodes) OR EXISTS (SELECT 1 FROM pv2_free_inbounds)) THEN
        WHILE EXISTS (SELECT 1 FROM location_groups WHERE name = nm) LOOP
            k := k + 1;
            nm := 'Общие входы ' || k;
        END LOOP;
        INSERT INTO location_groups (name) VALUES (nm) RETURNING id INTO gid;
        INSERT INTO group_nodes (group_id, node_id) SELECT gid, node_id FROM pv2_free_nodes;
        INSERT INTO group_inbounds (group_id, inbound_id) SELECT gid, inbound_id FROM pv2_free_inbounds;
        UPDATE plans SET location_group_id = gid WHERE location_group_id IS NULL;
    END IF;
END $$;

ALTER TABLE plan_pools DROP CONSTRAINT plan_pools_check;
ALTER TABLE plan_pools ADD CONSTRAINT plan_pools_id_plan UNIQUE (id, plan_id);

CREATE TABLE plan_pool_inbounds (
    plan_id    BIGINT NOT NULL,
    pool_id    BIGINT NOT NULL,
    inbound_id BIGINT NOT NULL REFERENCES node_inbounds(id) ON DELETE CASCADE,
    PRIMARY KEY (pool_id, inbound_id),
    UNIQUE (plan_id, inbound_id),
    FOREIGN KEY (pool_id, plan_id) REFERENCES plan_pools (id, plan_id) ON DELETE CASCADE
);
CREATE INDEX idx_plan_pool_inbounds_inbound ON plan_pool_inbounds (inbound_id);

UPDATE plan_pools pp SET name = COALESCE(
    NULLIF((SELECT n.name FROM nodes n WHERE n.id = pp.node_id), ''),
    NULLIF((SELECT g.name FROM location_groups g WHERE g.id = pp.group_id), ''),
    'Пул')
WHERE pp.name = '';

INSERT INTO plan_pool_inbounds (plan_id, pool_id, inbound_id)
SELECT plan_id, pool_id, inbound_id FROM pv2_cover WHERE pool_id IS NOT NULL;

UPDATE plan_pools SET position = position + 1
WHERE plan_id IN (SELECT id FROM plans WHERE traffic_limit_bytes IS NOT NULL);

CREATE TEMP TABLE pv2_main (pool_id BIGINT, plan_id BIGINT);

WITH ins AS (
    INSERT INTO plan_pools (plan_id, position, name, traffic_limit_bytes)
    SELECT id, 0, 'Основной', traffic_limit_bytes FROM plans WHERE traffic_limit_bytes IS NOT NULL
    RETURNING id, plan_id)
INSERT INTO pv2_main SELECT id, plan_id FROM ins;

INSERT INTO plan_pool_inbounds (plan_id, pool_id, inbound_id)
SELECT c.plan_id, m.pool_id, c.inbound_id FROM pv2_cover c JOIN pv2_main m ON m.plan_id = c.plan_id
WHERE c.pool_id IS NULL;

ALTER TABLE period_pool_usage DISABLE TRIGGER trg_period_pool_bonus_insert_bump;
INSERT INTO period_pool_usage (period_id, pool_id, used_bytes, bonus_bytes)
SELECT s.id, m.pool_id, s.traffic_used_bytes, s.traffic_bonus_bytes
FROM subscriptions s JOIN pv2_main m ON m.plan_id = s.plan_id
WHERE s.traffic_used_bytes > 0 OR s.traffic_bonus_bytes > 0;
ALTER TABLE period_pool_usage ENABLE TRIGGER trg_period_pool_bonus_insert_bump;

UPDATE subscriptions s SET traffic_used_bytes = 0
FROM pv2_main m WHERE m.plan_id = s.plan_id AND s.traffic_used_bytes <> 0;

DROP FUNCTION period_exhausted(BIGINT);
DROP VIEW period_pools;
DROP VIEW plan_inbounds;

DROP TRIGGER trg_plans_bump ON plans;
ALTER TABLE plans DROP COLUMN traffic_limit_bytes;
CREATE TRIGGER trg_plans_bump
AFTER UPDATE ON plans
FOR EACH ROW
WHEN (OLD.location_group_id IS DISTINCT FROM NEW.location_group_id
      OR OLD.dedicated_ip IS DISTINCT FROM NEW.dedicated_ip)
EXECUTE FUNCTION bump_nodes_version();

DROP TRIGGER trg_subscriptions_bump ON subscriptions;
ALTER TABLE subscriptions DROP COLUMN traffic_bonus_bytes;
CREATE TRIGGER trg_subscriptions_bump
AFTER INSERT OR DELETE OR UPDATE OF user_id, plan_id, starts_at, expires_at, canceled ON subscriptions
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();

ALTER TABLE plan_pools DROP COLUMN node_id, DROP COLUMN group_id;
ALTER TABLE plan_pools ADD CONSTRAINT plan_pools_name_check CHECK (name <> '');

CREATE TRIGGER trg_plan_pool_inbounds_insert_bump AFTER INSERT ON plan_pool_inbounds
REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION bump_member_nodes();
CREATE TRIGGER trg_plan_pool_inbounds_delete_bump AFTER DELETE ON plan_pool_inbounds
REFERENCING OLD TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION bump_member_nodes();

CREATE VIEW plan_inbounds AS
    SELECT p.id AS plan_id, m.inbound_id, i.node_id, ppi.pool_id, COALESCE(ppi.pool_id, 0) AS pool
    FROM plans p
    JOIN inbound_memberships m ON m.group_id = p.location_group_id
    JOIN node_inbounds i ON i.id = m.inbound_id
    LEFT JOIN plan_pool_inbounds ppi ON ppi.plan_id = p.id AND ppi.inbound_id = m.inbound_id;

CREATE VIEW period_pools AS
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

CREATE FUNCTION period_exhausted(period BIGINT) RETURNS BOOLEAN AS $$
    WITH rel AS (
        SELECT DISTINCT pi.pool FROM plan_inbounds pi JOIN subscriptions s ON s.plan_id = pi.plan_id WHERE s.id = period)
    SELECT EXISTS (SELECT 1 FROM rel)
       AND NOT EXISTS (
           SELECT 1 FROM rel
           LEFT JOIN period_pools x ON x.period_id = period AND COALESCE(x.pool_id, 0) = rel.pool
           WHERE x.limit_bytes IS NULL OR x.used_bytes < x.limit_bytes)
$$ LANGUAGE sql STABLE;

DROP TABLE pv2_main;
DROP TABLE pv2_free_inbounds;
DROP TABLE pv2_free_nodes;
DROP TABLE pv2_cover;

UPDATE nodes SET users_version = next_users_version(users_version);
