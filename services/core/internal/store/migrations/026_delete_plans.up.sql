ALTER TABLE plans ADD COLUMN deleted BOOLEAN NOT NULL DEFAULT FALSE CHECK (NOT deleted OR NOT on_sale);
ALTER TABLE plans DROP CONSTRAINT plans_name_key;
CREATE UNIQUE INDEX plans_live_name ON plans (name) WHERE NOT deleted;
