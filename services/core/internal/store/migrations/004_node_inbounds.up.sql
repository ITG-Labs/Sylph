ALTER TABLE nodes
    ADD COLUMN address      TEXT NOT NULL DEFAULT '',
    ADD COLUMN config_error TEXT;

ALTER TABLE devices
    ADD COLUMN device_os    TEXT NOT NULL DEFAULT '',
    ADD COLUMN device_model TEXT NOT NULL DEFAULT '';

DROP TRIGGER trg_nodes_self_bump ON nodes;
ALTER TABLE nodes DROP COLUMN config;
CREATE TRIGGER trg_nodes_self_bump
BEFORE UPDATE OF group_id ON nodes
FOR EACH ROW
WHEN (OLD.group_id IS DISTINCT FROM NEW.group_id)
EXECUTE FUNCTION bump_own_node_version();

CREATE TABLE node_inbounds (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_id     BIGINT NOT NULL REFERENCES nodes(id),
    tag         TEXT   NOT NULL,
    port        INT    NOT NULL,
    protocol    TEXT   NOT NULL,
    transport   TEXT   NOT NULL,
    security    TEXT   NOT NULL,
    label       TEXT   NOT NULL DEFAULT '',
    settings    JSONB  NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (node_id, tag)
);

CREATE FUNCTION bump_inbound_node_version() RETURNS trigger AS $$
BEGIN
    UPDATE nodes SET users_version = next_users_version(users_version)
    WHERE id = CASE WHEN TG_OP = 'DELETE' THEN OLD.node_id ELSE NEW.node_id END;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_node_inbounds_write_bump
AFTER INSERT OR DELETE ON node_inbounds
FOR EACH ROW EXECUTE FUNCTION bump_inbound_node_version();

CREATE TRIGGER trg_node_inbounds_update_bump
AFTER UPDATE ON node_inbounds
FOR EACH ROW
WHEN (OLD.* IS DISTINCT FROM NEW.*)
EXECUTE FUNCTION bump_inbound_node_version();
