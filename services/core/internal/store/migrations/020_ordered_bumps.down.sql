CREATE OR REPLACE FUNCTION bump_nodes_version() RETURNS trigger AS $$
BEGIN
    UPDATE nodes SET users_version = next_users_version(users_version);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION bump_nodes_if_changed() RETURNS trigger AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM changed) THEN
        UPDATE nodes SET users_version = next_users_version(users_version);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION bump_member_nodes() RETURNS trigger AS $$
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

CREATE OR REPLACE FUNCTION bump_inbound_node_version() RETURNS trigger AS $$
DECLARE
    target BIGINT := CASE WHEN TG_OP = 'DELETE' THEN OLD.node_id ELSE NEW.node_id END;
BEGIN
    UPDATE nodes SET users_version = next_users_version(users_version)
    WHERE id = target OR id IN (SELECT from_node_id FROM node_links WHERE to_node_id = target);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP FUNCTION bump_nodes_ordered(BIGINT[]);
