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
