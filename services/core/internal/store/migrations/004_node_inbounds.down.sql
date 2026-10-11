DROP TABLE IF EXISTS node_inbounds;
DROP FUNCTION IF EXISTS bump_inbound_node_version();
DROP TRIGGER IF EXISTS trg_nodes_self_bump ON nodes;
ALTER TABLE nodes ADD COLUMN config JSONB NOT NULL DEFAULT '{}';
CREATE TRIGGER trg_nodes_self_bump
BEFORE UPDATE OF group_id, config ON nodes
FOR EACH ROW
WHEN (OLD.group_id IS DISTINCT FROM NEW.group_id OR OLD.config IS DISTINCT FROM NEW.config)
EXECUTE FUNCTION bump_own_node_version();
ALTER TABLE nodes
    DROP COLUMN IF EXISTS config_error,
    DROP COLUMN IF EXISTS address;
ALTER TABLE devices
    DROP COLUMN IF EXISTS device_model,
    DROP COLUMN IF EXISTS device_os;
