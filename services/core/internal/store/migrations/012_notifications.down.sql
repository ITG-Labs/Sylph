DROP TABLE notifications_sent;
DROP INDEX idx_subscriptions_expires;
ALTER TABLE subscriptions DROP COLUMN closed_manually;
ALTER TABLE settings
    DROP COLUMN notify_traffic,
    DROP COLUMN notify_expiry,
    DROP COLUMN notify_trial;
