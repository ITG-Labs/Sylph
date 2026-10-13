DROP TABLE admin_audit;

DROP TRIGGER trg_plans_bump ON plans;
CREATE TRIGGER trg_plans_bump AFTER INSERT OR UPDATE OR DELETE ON plans
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();

DROP INDEX idx_payments_paid;
DROP INDEX idx_payments_created;
UPDATE payments SET status = 'canceled' WHERE status IN ('refunded', 'rejected');
ALTER TABLE payments DROP CONSTRAINT payments_status_check;
ALTER TABLE payments ADD CONSTRAINT payments_status_check CHECK (status IN ('pending', 'succeeded', 'canceled', 'mismatch'));
ALTER TABLE payments
    DROP COLUMN refunded_at,
    DROP COLUMN note,
    DROP COLUMN period_id;

DROP TRIGGER trg_subscriptions_bump ON subscriptions;
CREATE TRIGGER trg_subscriptions_bump
AFTER INSERT OR DELETE OR UPDATE OF user_id, plan_id, starts_at, expires_at, canceled ON subscriptions
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();
ALTER TABLE subscriptions DROP COLUMN traffic_bonus_bytes;

ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users
    DROP COLUMN ban_reason,
    DROP COLUMN banned_at;
