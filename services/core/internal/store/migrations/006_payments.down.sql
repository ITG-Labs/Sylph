ALTER TABLE plans DROP COLUMN on_sale;

DROP INDEX idx_payments_user_pending;
DROP INDEX idx_payments_pending;
ALTER TABLE payments DROP CONSTRAINT payments_status_check;
ALTER TABLE payments DROP CONSTRAINT payments_source_check;
ALTER TABLE payments DROP COLUMN paid_at;
ALTER TABLE payments DROP COLUMN confirmation_url;
ALTER TABLE payments DROP COLUMN source;
