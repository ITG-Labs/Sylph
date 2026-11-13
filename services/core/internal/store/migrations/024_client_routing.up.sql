ALTER TABLE settings
    ADD COLUMN client_rules JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN client_rules_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN client_rules_updated_at TIMESTAMPTZ,
    ADD COLUMN client_geo_tags JSONB;
