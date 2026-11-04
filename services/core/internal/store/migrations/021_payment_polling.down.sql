DROP INDEX IF EXISTS idx_payments_poll;
ALTER TABLE payments DROP COLUMN IF EXISTS last_polled_at;
