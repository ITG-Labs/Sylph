DROP TRIGGER IF EXISTS trg_users_bump ON users;
DROP TRIGGER IF EXISTS trg_user_keys_bump ON user_keys;
DROP TRIGGER IF EXISTS trg_subscriptions_bump ON subscriptions;
DROP TRIGGER IF EXISTS trg_plans_bump ON plans;
DROP FUNCTION IF EXISTS bump_nodes_version();
