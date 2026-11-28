ALTER TABLE plans ADD CONSTRAINT plans_name_key UNIQUE (name);
DROP INDEX plans_live_name;
ALTER TABLE plans DROP COLUMN deleted;
