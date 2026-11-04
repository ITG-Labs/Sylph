DROP INDEX IF EXISTS idx_payments_unnotified;
ALTER TABLE payments DROP COLUMN IF EXISTS notified_at;
