CREATE TABLE online_now (
    node_id  BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip       TEXT NOT NULL CHECK (ip <> ''),
    since    TIMESTAMPTZ NOT NULL,
    seen_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (node_id, user_id, ip)
);
CREATE INDEX idx_online_now_user ON online_now (user_id, seen_at);

CREATE TABLE ip_history (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id    BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    ip         TEXT NOT NULL CHECK (ip <> ''),
    first_seen TIMESTAMPTZ NOT NULL,
    last_seen  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, node_id, ip)
);
CREATE INDEX idx_ip_history_user_last ON ip_history (user_id, last_seen DESC);
CREATE INDEX idx_ip_history_last ON ip_history (last_seen);

CREATE TABLE user_presence (
    user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    last_seen    TIMESTAMPTZ NOT NULL,
    last_node_id BIGINT REFERENCES nodes(id) ON DELETE SET NULL
);
