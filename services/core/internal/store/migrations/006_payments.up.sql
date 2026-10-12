ALTER TABLE payments ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE payments ADD COLUMN confirmation_url TEXT;
ALTER TABLE payments ADD COLUMN paid_at TIMESTAMPTZ;
UPDATE payments SET paid_at = created_at WHERE status = 'succeeded';
ALTER TABLE payments ADD CONSTRAINT payments_source_check CHECK (source IN ('manual', 'yookassa'));
ALTER TABLE payments ADD CONSTRAINT payments_status_check CHECK (status IN ('pending', 'succeeded', 'canceled', 'mismatch'));
CREATE INDEX idx_payments_pending ON payments (created_at) WHERE status = 'pending';
CREATE INDEX idx_payments_user_pending ON payments (user_id, plan_id) WHERE status = 'pending';

ALTER TABLE plans ADD COLUMN on_sale BOOLEAN NOT NULL DEFAULT TRUE;
