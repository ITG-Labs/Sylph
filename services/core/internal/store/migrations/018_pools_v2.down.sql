DROP FUNCTION period_exhausted(BIGINT);
DROP VIEW period_pools;
DROP VIEW plan_inbounds;

DO $$
DECLARE
    gid BIGINT;
    nm  TEXT := 'Скрытые входы';
    k   INT  := 1;
BEGIN
    IF EXISTS (SELECT 1 FROM node_inbounds i WHERE NOT EXISTS (SELECT 1 FROM inbound_memberships m WHERE m.inbound_id = i.id))
       OR EXISTS (SELECT 1 FROM nodes n WHERE NOT EXISTS (SELECT 1 FROM group_nodes gn WHERE gn.node_id = n.id)
                                          AND NOT EXISTS (SELECT 1 FROM group_inbounds gi JOIN node_inbounds i ON i.id = gi.inbound_id WHERE i.node_id = n.id)) THEN
        WHILE EXISTS (SELECT 1 FROM location_groups WHERE name = nm) LOOP
            k := k + 1;
            nm := 'Скрытые входы ' || k;
        END LOOP;
        INSERT INTO location_groups (name) VALUES (nm) RETURNING id INTO gid;
        INSERT INTO group_nodes (group_id, node_id)
        SELECT gid, n.id FROM nodes n
        WHERE NOT EXISTS (SELECT 1 FROM group_nodes gn WHERE gn.node_id = n.id)
          AND NOT EXISTS (SELECT 1 FROM group_inbounds gi JOIN node_inbounds i ON i.id = gi.inbound_id WHERE i.node_id = n.id);
        INSERT INTO group_inbounds (group_id, inbound_id)
        SELECT gid, i.id FROM node_inbounds i
        WHERE NOT EXISTS (SELECT 1 FROM inbound_memberships m WHERE m.inbound_id = i.id);
    END IF;
END $$;

ALTER TABLE plan_pools DROP CONSTRAINT plan_pools_name_check;
ALTER TABLE plan_pools ADD COLUMN node_id BIGINT REFERENCES nodes(id);
ALTER TABLE plan_pools ADD COLUMN group_id BIGINT REFERENCES location_groups(id);

DO $$
DECLARE
    r    RECORD;
    gid  BIGINT;
    base TEXT;
    nm   TEXT;
    k    INT;
BEGIN
    FOR r IN SELECT pp.id, pp.name, p.name AS plan FROM plan_pools pp JOIN plans p ON p.id = pp.plan_id ORDER BY pp.id LOOP
        base := 'Пул ' || r.plan || '/' || r.name;
        nm := base;
        k := 1;
        WHILE EXISTS (SELECT 1 FROM location_groups WHERE name = nm) LOOP
            k := k + 1;
            nm := base || ' ' || k;
        END LOOP;
        INSERT INTO location_groups (name) VALUES (nm) RETURNING id INTO gid;
        INSERT INTO group_inbounds (group_id, inbound_id) SELECT gid, inbound_id FROM plan_pool_inbounds WHERE pool_id = r.id;
        UPDATE plan_pools SET group_id = gid WHERE id = r.id;
    END LOOP;
END $$;

ALTER TABLE plan_pools ADD CHECK ((node_id IS NULL) <> (group_id IS NULL));
CREATE INDEX idx_plan_pools_group ON plan_pools (group_id) WHERE group_id IS NOT NULL;

DROP TABLE plan_pool_inbounds;
ALTER TABLE plan_pools DROP CONSTRAINT plan_pools_id_plan;

DROP TRIGGER trg_subscriptions_bump ON subscriptions;
ALTER TABLE subscriptions ADD COLUMN traffic_bonus_bytes BIGINT NOT NULL DEFAULT 0 CHECK (traffic_bonus_bytes >= 0);
CREATE TRIGGER trg_subscriptions_bump
AFTER INSERT OR DELETE OR UPDATE OF user_id, plan_id, starts_at, expires_at, canceled, traffic_bonus_bytes ON subscriptions
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();

DROP TRIGGER trg_plans_bump ON plans;
ALTER TABLE plans ADD COLUMN traffic_limit_bytes BIGINT;
CREATE TRIGGER trg_plans_bump
AFTER UPDATE ON plans
FOR EACH ROW
WHEN (OLD.location_group_id IS DISTINCT FROM NEW.location_group_id
      OR OLD.traffic_limit_bytes IS DISTINCT FROM NEW.traffic_limit_bytes
      OR OLD.dedicated_ip IS DISTINCT FROM NEW.dedicated_ip)
EXECUTE FUNCTION bump_nodes_version();

CREATE VIEW plan_inbounds AS
    WITH m AS MATERIALIZED (SELECT group_id, inbound_id FROM inbound_memberships),
    visible AS (
        SELECT p.id AS plan_id, i.id AS inbound_id, i.node_id
        FROM plans p CROSS JOIN node_inbounds i
        WHERE NOT EXISTS (SELECT 1 FROM m WHERE m.inbound_id = i.id)
        UNION
        SELECT p.id, i.id, i.node_id
        FROM plans p JOIN m ON m.group_id = p.location_group_id JOIN node_inbounds i ON i.id = m.inbound_id),
    covered AS (
        SELECT DISTINCT ON (c.plan_id, c.inbound_id) c.plan_id, c.inbound_id, c.pool_id
        FROM (SELECT pp.plan_id, i.id AS inbound_id, pp.id AS pool_id, pp.position
              FROM plan_pools pp JOIN node_inbounds i ON i.node_id = pp.node_id
              UNION ALL
              SELECT pp.plan_id, m.inbound_id, pp.id, pp.position
              FROM plan_pools pp JOIN m ON m.group_id = pp.group_id) c
        ORDER BY c.plan_id, c.inbound_id, c.position, c.pool_id)
    SELECT v.plan_id, v.inbound_id, v.node_id, c.pool_id, COALESCE(c.pool_id, 0) AS pool
    FROM visible v LEFT JOIN covered c ON c.plan_id = v.plan_id AND c.inbound_id = v.inbound_id;

CREATE VIEW period_pools AS
    SELECT s.id AS period_id, NULL::BIGINT AS pool_id, -1 AS position, s.traffic_used_bytes AS used_bytes, s.traffic_bonus_bytes AS bonus_bytes,
           p.traffic_limit_bytes AS base_limit_bytes,
           CASE WHEN p.traffic_limit_bytes IS NOT NULL
                THEN LEAST(p.traffic_limit_bytes::numeric + s.traffic_bonus_bytes, 9223372036854775807)::BIGINT END AS limit_bytes
    FROM subscriptions s JOIN plans p ON p.id = s.plan_id
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
        SELECT DISTINCT pi.pool FROM plan_inbounds pi JOIN subscriptions s ON s.plan_id = pi.plan_id WHERE s.id = period),
    pools AS (
        SELECT COALESCE(x.pool_id, 0) AS pool, x.limit_bytes IS NOT NULL AND x.used_bytes >= x.limit_bytes AS spent
        FROM period_pools x WHERE x.period_id = period)
    SELECT CASE WHEN EXISTS (SELECT 1 FROM pools JOIN rel USING (pool))
                THEN NOT EXISTS (SELECT 1 FROM pools JOIN rel USING (pool) WHERE NOT spent)
                ELSE COALESCE((SELECT spent FROM pools WHERE pool = 0), FALSE)
           END
$$ LANGUAGE sql STABLE;

UPDATE nodes SET users_version = next_users_version(users_version);
