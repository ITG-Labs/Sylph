CREATE TABLE routing_profiles (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    doc         JSONB NOT NULL,
    revision    BIGINT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE nodes
    ADD COLUMN profile_id    BIGINT REFERENCES routing_profiles(id),
    ADD COLUMN geo_status    JSONB,
    ADD COLUMN routing_error TEXT;
CREATE INDEX idx_nodes_profile ON nodes (profile_id) WHERE profile_id IS NOT NULL;

CREATE TABLE node_links (
    from_node_id BIGINT NOT NULL REFERENCES nodes(id),
    to_node_id   BIGINT NOT NULL REFERENCES nodes(id),
    key_uuid     TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (from_node_id, to_node_id),
    CHECK (from_node_id <> to_node_id)
);
CREATE INDEX idx_node_links_to ON node_links (to_node_id);

CREATE TABLE geo_sources (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    kind        TEXT NOT NULL CHECK (kind IN ('domain', 'ip')),
    url         TEXT NOT NULL,
    sha256_url  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE settings ADD COLUMN geo_generation BIGINT NOT NULL DEFAULT 0;

DROP TRIGGER trg_nodes_self_bump ON nodes;
CREATE TRIGGER trg_nodes_self_bump
BEFORE UPDATE OF group_id, profile_id ON nodes
FOR EACH ROW
WHEN (OLD.group_id IS DISTINCT FROM NEW.group_id OR OLD.profile_id IS DISTINCT FROM NEW.profile_id)
EXECUTE FUNCTION bump_own_node_version();
