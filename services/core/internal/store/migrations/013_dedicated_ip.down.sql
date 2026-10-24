DROP TABLE address_assignments;
DROP TABLE node_addresses;
DROP FUNCTION bump_address_node_version();

DROP TRIGGER trg_plans_bump ON plans;
CREATE TRIGGER trg_plans_bump
AFTER UPDATE ON plans
FOR EACH ROW
WHEN (OLD.location_group_id IS DISTINCT FROM NEW.location_group_id OR OLD.traffic_limit_bytes IS DISTINCT FROM NEW.traffic_limit_bytes)
EXECUTE FUNCTION bump_nodes_version();

ALTER TABLE plans DROP COLUMN dedicated_ip;
