DROP FUNCTION period_exhausted(BIGINT);
DROP VIEW period_pools;
DROP VIEW plan_inbounds;
DROP VIEW inbound_memberships;

UPDATE subscriptions s
SET traffic_used_bytes = LEAST(s.traffic_used_bytes::numeric + u.used, 9223372036854775807)::BIGINT
FROM (SELECT period_id, sum(used_bytes) AS used FROM period_pool_usage GROUP BY period_id) u
WHERE u.period_id = s.id AND u.used > 0;

DROP TABLE period_pool_usage;
DROP TABLE plan_pools;

ALTER TABLE nodes ADD COLUMN group_id BIGINT REFERENCES location_groups(id);
UPDATE nodes n SET group_id = COALESCE(
    (SELECT min(gn.group_id) FROM group_nodes gn WHERE gn.node_id = n.id),
    (SELECT min(gi.group_id) FROM group_inbounds gi JOIN node_inbounds i ON i.id = gi.inbound_id WHERE i.node_id = n.id));

DROP TRIGGER trg_nodes_self_bump ON nodes;
CREATE TRIGGER trg_nodes_self_bump
BEFORE UPDATE OF group_id, profile_id ON nodes
FOR EACH ROW
WHEN (OLD.group_id IS DISTINCT FROM NEW.group_id OR OLD.profile_id IS DISTINCT FROM NEW.profile_id)
EXECUTE FUNCTION bump_own_node_version();

DROP TABLE group_inbounds;
DROP TABLE group_nodes;
DROP FUNCTION bump_nodes_if_changed();
DROP FUNCTION bump_member_nodes();

UPDATE nodes SET users_version = next_users_version(users_version);
