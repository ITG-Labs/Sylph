ALTER TABLE payments ADD COLUMN notified_at TIMESTAMPTZ;
UPDATE payments SET notified_at = now() WHERE status <> 'pending';
CREATE INDEX idx_payments_unnotified ON payments (paid_at) WHERE status = 'succeeded' AND source = 'yookassa' AND notified_at IS NULL;
