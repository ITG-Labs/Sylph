ALTER TABLE traffic_daily ADD COLUMN inbound_tag TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_daily DROP CONSTRAINT traffic_daily_pkey;
ALTER TABLE traffic_daily ADD PRIMARY KEY (user_id, day, node_id, period_id, inbound_tag);
