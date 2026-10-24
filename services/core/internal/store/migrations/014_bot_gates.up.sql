ALTER TABLE settings
    ADD COLUMN terms_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN terms_text      TEXT NOT NULL DEFAULT '',
    ADD COLUMN terms_url       TEXT NOT NULL DEFAULT '',
    ADD COLUMN terms_version   INTEGER NOT NULL DEFAULT 1 CHECK (terms_version >= 1),
    ADD COLUMN channel_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN channel_chat    TEXT NOT NULL DEFAULT '',
    ADD COLUMN channel_url     TEXT NOT NULL DEFAULT '',
    ADD COLUMN channel_text    TEXT NOT NULL DEFAULT '',
    ADD COLUMN help_enabled    BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN help_text       TEXT NOT NULL DEFAULT '',
    ADD COLUMN help_buttons    JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(help_buttons) = 'array'),
    ADD CONSTRAINT settings_terms_ready CHECK (NOT terms_enabled OR terms_url <> ''),
    ADD CONSTRAINT settings_channel_ready CHECK (NOT channel_enabled OR (channel_chat <> '' AND channel_url <> '')),
    ADD CONSTRAINT settings_help_ready CHECK (NOT help_enabled OR help_text <> '');

CREATE TABLE terms_acceptances (
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL CHECK (version >= 1),
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, version)
);
