DROP TABLE notifications_sent;
ALTER TABLE settings
    DROP COLUMN notify_traffic,
    DROP COLUMN notify_expiry,
    DROP COLUMN notify_trial;
