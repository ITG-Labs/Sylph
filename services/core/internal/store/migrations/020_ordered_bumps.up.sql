CREATE FUNCTION bump_nodes_ordered(ids BIGINT[]) RETURNS void AS $$
BEGIN
    PERFORM 1 FROM nodes WHERE ids IS NULL OR id = ANY (ids) ORDER BY id FOR NO KEY UPDATE;
    UPDATE nodes SET users_version = next_users_version(users_version) WHERE ids IS NULL OR id = ANY (ids);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION bump_nodes_version() RETURNS trigger AS $$
BEGIN
    PERFORM bump_nodes_ordered(NULL);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION bump_nodes_if_changed() RETURNS trigger AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM changed) THEN
        PERFORM bump_nodes_ordered(NULL);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION bump_member_nodes() RETURNS trigger AS $$
BEGIN
    IF TG_TABLE_NAME = 'group_nodes' THEN
        PERFORM bump_nodes_ordered(ARRAY(SELECT node_id FROM changed));
    ELSE
        PERFORM bump_nodes_ordered(ARRAY(SELECT i.node_id FROM node_inbounds i WHERE i.id IN (SELECT inbound_id FROM changed)));
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION bump_inbound_node_version() RETURNS trigger AS $$
DECLARE
    target BIGINT := CASE WHEN TG_OP = 'DELETE' THEN OLD.node_id ELSE NEW.node_id END;
BEGIN
    PERFORM bump_nodes_ordered(target || ARRAY(SELECT from_node_id FROM node_links WHERE to_node_id = target));
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
