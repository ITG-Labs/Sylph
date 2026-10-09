CREATE OR REPLACE FUNCTION bump_nodes_version() RETURNS trigger AS $$
BEGIN
    UPDATE nodes SET users_version = users_version + 1;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_bump AFTER INSERT OR UPDATE OR DELETE ON users
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();
CREATE TRIGGER trg_user_keys_bump AFTER INSERT OR UPDATE OR DELETE ON user_keys
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();
CREATE TRIGGER trg_subscriptions_bump AFTER INSERT OR UPDATE OR DELETE ON subscriptions
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();
CREATE TRIGGER trg_plans_bump AFTER INSERT OR UPDATE OR DELETE ON plans
FOR EACH STATEMENT EXECUTE FUNCTION bump_nodes_version();
