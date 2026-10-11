CREATE TABLE traffic_batches (
    node_id     BIGINT NOT NULL REFERENCES nodes(id),
    batch_id    TEXT   NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (node_id, batch_id)
);

ALTER TABLE nodes
    ADD COLUMN active_users     INT,
    ADD COLUMN agent_uptime_sec BIGINT,
    ADD COLUMN xray_uptime_sec  BIGINT,
    ADD COLUMN last_sync_at     TIMESTAMPTZ;

CREATE FUNCTION next_users_version(v BIGINT) RETURNS BIGINT AS $$
    SELECT GREATEST(v + 1, (extract(epoch FROM clock_timestamp()) * 1000000)::BIGINT)
$$ LANGUAGE sql VOLATILE;

CREATE OR REPLACE FUNCTION bump_nodes_version() RETURNS trigger AS $$
BEGIN
    UPDATE nodes SET users_version = next_users_version(users_version);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION bump_own_node_version() RETURNS trigger AS $$
BEGIN
    NEW.users_version := next_users_version(OLD.users_version);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_nodes_self_bump
BEFORE UPDATE OF group_id, config ON nodes
FOR EACH ROW
WHEN (OLD.group_id IS DISTINCT FROM NEW.group_id OR OLD.config IS DISTINCT FROM NEW.config)
EXECUTE FUNCTION bump_own_node_version();
