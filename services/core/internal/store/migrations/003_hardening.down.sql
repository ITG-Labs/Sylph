DROP TRIGGER IF EXISTS trg_nodes_self_bump ON nodes;
DROP FUNCTION IF EXISTS bump_own_node_version();
CREATE OR REPLACE FUNCTION bump_nodes_version() RETURNS trigger AS $$
BEGIN
    UPDATE nodes SET users_version = users_version + 1;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
DROP FUNCTION IF EXISTS next_users_version(BIGINT);
ALTER TABLE nodes
    DROP COLUMN IF EXISTS last_sync_at,
    DROP COLUMN IF EXISTS xray_uptime_sec,
    DROP COLUMN IF EXISTS agent_uptime_sec,
    DROP COLUMN IF EXISTS active_users;
DROP TABLE IF EXISTS traffic_batches;
