CREATE TABLE group_nodes (
    group_id BIGINT NOT NULL REFERENCES location_groups(id),
    node_id  BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, node_id)
);
CREATE INDEX idx_group_nodes_node ON group_nodes (node_id);

CREATE TABLE group_inbounds (
    group_id   BIGINT NOT NULL REFERENCES location_groups(id),
    inbound_id BIGINT NOT NULL REFERENCES node_inbounds(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, inbound_id)
);
CREATE INDEX idx_group_inbounds_inbound ON group_inbounds (inbound_id);

INSERT INTO group_nodes (group_id, node_id) SELECT group_id, id FROM nodes WHERE group_id IS NOT NULL;

ALTER TABLE nodes ADD COLUMN agent_per_inbound BOOLEAN NOT NULL DEFAULT FALSE;

DROP TRIGGER trg_nodes_self_bump ON nodes;
ALTER TABLE nodes DROP COLUMN group_id;
CREATE TRIGGER trg_nodes_self_bump
BEFORE UPDATE OF profile_id ON nodes
FOR EACH ROW
WHEN (OLD.profile_id IS DISTINCT FROM NEW.profile_id)
EXECUTE FUNCTION bump_own_node_version();

CREATE FUNCTION bump_nodes_if_changed() RETURNS trigger AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM changed) THEN
        UPDATE nodes SET users_version = next_users_version(users_version);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION bump_member_nodes() RETURNS trigger AS $$
BEGIN
    IF TG_TABLE_NAME = 'group_nodes' THEN
        UPDATE nodes SET users_version = next_users_version(users_version)
        WHERE id IN (SELECT node_id FROM changed);
    ELSE
        UPDATE nodes SET users_version = next_users_version(users_version)
        WHERE id IN (SELECT i.node_id FROM node_inbounds i WHERE i.id IN (SELECT inbound_id FROM changed));
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_group_nodes_insert_bump AFTER INSERT ON group_nodes
REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION bump_member_nodes();
CREATE TRIGGER trg_group_nodes_delete_bump AFTER DELETE ON group_nodes
REFERENCING OLD TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION bump_member_nodes();
CREATE TRIGGER trg_group_inbounds_insert_bump AFTER INSERT ON group_inbounds
REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION bump_member_nodes();
CREATE TRIGGER trg_group_inbounds_delete_bump AFTER DELETE ON group_inbounds
REFERENCING OLD TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION bump_member_nodes();

CREATE TABLE plan_pools (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    plan_id             BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    position            INT    NOT NULL,
    name                TEXT   NOT NULL DEFAULT '',
    node_id             BIGINT REFERENCES nodes(id),
    group_id            BIGINT REFERENCES location_groups(id),
    traffic_limit_bytes BIGINT CHECK (traffic_limit_bytes IS NULL OR traffic_limit_bytes > 0),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((node_id IS NULL) <> (group_id IS NULL))
);
CREATE INDEX idx_plan_pools_plan ON plan_pools (plan_id, position, id);
CREATE INDEX idx_plan_pools_group ON plan_pools (group_id) WHERE group_id IS NOT NULL;

CREATE TRIGGER trg_plan_pools_insert_bump AFTER INSERT ON plan_pools
REFERENCING NEW TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_if_changed();
CREATE TRIGGER trg_plan_pools_delete_bump AFTER DELETE ON plan_pools
REFERENCING OLD TABLE AS changed FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_if_changed();
CREATE TRIGGER trg_plan_pools_update_bump AFTER UPDATE ON plan_pools
FOR EACH ROW WHEN (OLD.* IS DISTINCT FROM NEW.*)
EXECUTE FUNCTION bump_nodes_version();

CREATE TABLE period_pool_usage (
    period_id   BIGINT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    pool_id     BIGINT NOT NULL REFERENCES plan_pools(id) ON DELETE CASCADE,
    used_bytes  BIGINT NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    bonus_bytes BIGINT NOT NULL DEFAULT 0 CHECK (bonus_bytes >= 0),
    PRIMARY KEY (period_id, pool_id)
);
CREATE INDEX idx_period_pool_usage_pool ON period_pool_usage (pool_id);

CREATE TRIGGER trg_period_pool_bonus_insert_bump AFTER INSERT ON period_pool_usage
FOR EACH ROW WHEN (NEW.bonus_bytes > 0)
EXECUTE FUNCTION bump_nodes_version();
CREATE TRIGGER trg_period_pool_bonus_update_bump AFTER UPDATE OF bonus_bytes ON period_pool_usage
FOR EACH ROW WHEN (OLD.bonus_bytes IS DISTINCT FROM NEW.bonus_bytes)
EXECUTE FUNCTION bump_nodes_version();

CREATE VIEW inbound_memberships AS
    SELECT gi.group_id, gi.inbound_id FROM group_inbounds gi
    UNION
    SELECT gn.group_id, i.id FROM group_nodes gn JOIN node_inbounds i ON i.node_id = gn.node_id;

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
