ALTER TABLE payments ADD COLUMN last_polled_at TIMESTAMPTZ;
CREATE INDEX idx_payments_poll ON payments (last_polled_at ASC NULLS FIRST, created_at DESC) WHERE status = 'pending' AND source = 'yookassa';
