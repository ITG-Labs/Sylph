CREATE TEMP TABLE td19 AS
    SELECT user_id, day, node_id, period_id,
           LEAST(sum(bytes_up), 9223372036854775807)::BIGINT AS bytes_up,
           LEAST(sum(bytes_down), 9223372036854775807)::BIGINT AS bytes_down
    FROM traffic_daily GROUP BY user_id, day, node_id, period_id;
TRUNCATE traffic_daily;
ALTER TABLE traffic_daily DROP CONSTRAINT traffic_daily_pkey;
ALTER TABLE traffic_daily DROP COLUMN inbound_tag;
ALTER TABLE traffic_daily ADD PRIMARY KEY (user_id, day, node_id, period_id);
INSERT INTO traffic_daily (user_id, day, node_id, period_id, bytes_up, bytes_down)
SELECT user_id, day, node_id, period_id, bytes_up, bytes_down FROM td19;
DROP TABLE td19;
