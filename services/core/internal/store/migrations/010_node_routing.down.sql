CREATE OR REPLACE FUNCTION bump_inbound_node_version() RETURNS trigger AS $$
BEGIN
    UPDATE nodes SET users_version = next_users_version(users_version)
    WHERE id = CASE WHEN TG_OP = 'DELETE' THEN OLD.node_id ELSE NEW.node_id END;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER trg_nodes_self_bump ON nodes;
CREATE TRIGGER trg_nodes_self_bump
BEFORE UPDATE OF group_id ON nodes
FOR EACH ROW
WHEN (OLD.group_id IS DISTINCT FROM NEW.group_id)
EXECUTE FUNCTION bump_own_node_version();

ALTER TABLE settings DROP COLUMN geo_generation;
DROP TABLE geo_sources;
DROP TABLE node_links;
ALTER TABLE nodes
    DROP COLUMN routing_error,
    DROP COLUMN geo_status,
    DROP COLUMN profile_id;
DROP TABLE routing_profiles;
