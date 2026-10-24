ALTER TABLE plans ADD COLUMN dedicated_ip BOOLEAN NOT NULL DEFAULT FALSE;

DROP TRIGGER trg_plans_bump ON plans;
CREATE TRIGGER trg_plans_bump
AFTER UPDATE ON plans
FOR EACH ROW
WHEN (OLD.location_group_id IS DISTINCT FROM NEW.location_group_id
      OR OLD.traffic_limit_bytes IS DISTINCT FROM NEW.traffic_limit_bytes
      OR OLD.dedicated_ip IS DISTINCT FROM NEW.dedicated_ip)
EXECUTE FUNCTION bump_nodes_version();

CREATE TABLE node_addresses (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_id       BIGINT NOT NULL REFERENCES nodes(id),
    ip            TEXT   NOT NULL CHECK (ip <> ''),
    status        TEXT   NOT NULL CHECK (status IN ('ok', 'error', 'missing')),
    error         TEXT,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (node_id, ip),
    UNIQUE (id, node_id)
);

CREATE TABLE address_assignments (
    address_id  BIGINT PRIMARY KEY,
    node_id     BIGINT NOT NULL,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source      TEXT   NOT NULL DEFAULT 'admin' CHECK (source IN ('admin', 'purchase')),
    note        TEXT   NOT NULL DEFAULT '',
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (address_id, node_id) REFERENCES node_addresses(id, node_id) ON DELETE CASCADE,
    UNIQUE (user_id, node_id)
);

CREATE FUNCTION bump_address_node_version() RETURNS trigger AS $$
DECLARE
    target BIGINT := CASE WHEN TG_OP = 'DELETE' THEN OLD.node_id ELSE NEW.node_id END;
BEGIN
    UPDATE nodes SET users_version = next_users_version(users_version) WHERE id = target;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_node_addresses_write_bump
AFTER INSERT OR DELETE ON node_addresses
FOR EACH ROW EXECUTE FUNCTION bump_address_node_version();

CREATE TRIGGER trg_node_addresses_status_bump
AFTER UPDATE OF status ON node_addresses
FOR EACH ROW
WHEN (OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION bump_address_node_version();

CREATE TRIGGER trg_address_assignments_write_bump
AFTER INSERT OR DELETE ON address_assignments
FOR EACH ROW EXECUTE FUNCTION bump_address_node_version();

CREATE TRIGGER trg_address_assignments_user_bump
AFTER UPDATE OF user_id ON address_assignments
FOR EACH ROW
WHEN (OLD.user_id IS DISTINCT FROM NEW.user_id)
EXECUTE FUNCTION bump_address_node_version();
