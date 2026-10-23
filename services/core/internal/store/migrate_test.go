package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("SYLPH_TEST_DB")
	if url == "" {
		t.Skip("SYLPH_TEST_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestApplyMigrations(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name <> 'schema_migrations'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 25 {
		t.Fatalf("tables = %d, want 25", count)
	}
}

func TestApplyMigrationsIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestMigration003DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Migrate(2); err != nil {
		t.Fatalf("down to 002: %v", err)
	}
	if tableExists(t, pool, "traffic_batches") {
		t.Fatal("traffic_batches survived down migration")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO nodes (cert_cn, name) VALUES ('n1', 'n1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (telegram_id, sub_token) VALUES (1, 't1')`); err != nil {
		t.Fatalf("bump after down must work without next_users_version: %v", err)
	}
	var v int64
	if err := pool.QueryRow(ctx, `SELECT users_version FROM nodes WHERE cert_cn = 'n1'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("version after down = %d, want 1 (+1 counter)", v)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("up from 002 again: %v", err)
	}
	if !tableExists(t, pool, "traffic_batches") {
		t.Fatal("traffic_batches missing after up")
	}
}

func columnExists(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2)`,
		table, column).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestMigration004DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if columnExists(t, pool, "nodes", "config") || !columnExists(t, pool, "nodes", "address") || !columnExists(t, pool, "nodes", "config_error") ||
		!columnExists(t, pool, "devices", "device_os") || !columnExists(t, pool, "devices", "device_model") {
		t.Fatal("004 did not reshape nodes and devices")
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Migrate(3); err != nil {
		t.Fatalf("down to 003: %v", err)
	}
	if tableExists(t, pool, "node_inbounds") || columnExists(t, pool, "nodes", "address") || !columnExists(t, pool, "nodes", "config") ||
		columnExists(t, pool, "devices", "device_os") {
		t.Fatal("down 004 did not restore nodes and devices")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO nodes (cert_cn, name) VALUES ('n1', 'n1')`); err != nil {
		t.Fatal(err)
	}
	var before, after int64
	if err := pool.QueryRow(ctx, `SELECT users_version FROM nodes WHERE cert_cn = 'n1'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE nodes SET config = '{"a":1}' WHERE cert_cn = 'n1'`); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT users_version FROM nodes WHERE cert_cn = 'n1'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Fatalf("config bump trigger not restored by down: %d -> %d", before, after)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("up from 003 again: %v", err)
	}
	if !tableExists(t, pool, "node_inbounds") || columnExists(t, pool, "nodes", "config") {
		t.Fatal("up 004 again did not reshape nodes")
	}
}

func TestMigration005DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Steps(4); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO nodes (cert_cn, name) VALUES ('n1', 'n1')`,
		`INSERT INTO plans (name, price_rub, period_days) VALUES ('p', 1, 30)`,
		`INSERT INTO users (telegram_id, sub_token, status) VALUES (1, 't1', 'expired_traffic')`,
		`INSERT INTO user_keys (user_id, uuid) VALUES (1, 'k1')`,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) VALUES (1, 1, now(), now() + interval '30 days')`,
		`INSERT INTO traffic_deltas (node_id, key_uuid, bytes_up, bytes_down) VALUES (1, 'k1', 5, 5)`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 005: %v", err)
	}
	var status string
	var owner *int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT status FROM users WHERE id = 1), (SELECT user_id FROM traffic_deltas LIMIT 1)`).Scan(&status, &owner); err != nil {
		t.Fatal(err)
	}
	if status != "active" || owner == nil || *owner != 1 {
		t.Fatalf("status = %q, delta owner = %v", status, owner)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_keys SET revoked_at = now() WHERE uuid = 'k1'`); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 005: %v", err)
	}
	if columnExists(t, pool, "user_keys", "revoked_at") || columnExists(t, pool, "traffic_deltas", "user_id") {
		t.Fatal("down 005 left its columns")
	}
	var keys int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_keys`).Scan(&keys); err != nil || keys != 0 {
		t.Fatalf("revoked key survived down 005: %d, %v", keys, err)
	}
	var before, after int64
	if err := pool.QueryRow(ctx, `SELECT users_version FROM nodes WHERE id = 1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE subscriptions SET traffic_used_bytes = 7`); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT users_version FROM nodes WHERE id = 1`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Fatal("down 005 did not restore the old subscriptions trigger")
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 005 again: %v", err)
	}
}

func TestMigration005DownKeepsLimitsForQueuedPeriods(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO plans (name, price_rub, period_days, traffic_limit_bytes) VALUES ('p', 1, 30, 1000)`,
		`INSERT INTO users (telegram_id, sub_token, status) VALUES (1, 't1', 'active'), (2, 't2', 'active')`,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at, traffic_used_bytes) VALUES
		     (1, 1, now() - interval '1 day', now() + interval '29 days', 1500),
		     (1, 1, now() + interval '29 days', now() + interval '59 days', 0),
		     (1, 1, now() - interval '40 days', now() - interval '10 days', 700),
		     (2, 1, now() - interval '1 day', now() + interval '29 days', 300),
		     (2, 1, now() + interval '29 days', now() + interval '59 days', 0)`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Migrate(4); err != nil {
		t.Fatalf("down to 004: %v", err)
	}
	type row struct {
		starts, expires string
		used            int64
	}
	for user, want := range map[int]struct {
		status string
		used   int64
	}{1: {"expired_traffic", 1500}, 2: {"active", 300}} {
		var status string
		var live int
		var r row
		if err := pool.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, user).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx,
			`SELECT count(*), min(starts_at)::date::text, max(expires_at)::date::text, max(traffic_used_bytes)
			 FROM subscriptions WHERE user_id = $1 AND NOT canceled AND expires_at > now()`, user).Scan(&live, &r.starts, &r.expires, &r.used); err != nil {
			t.Fatal(err)
		}
		var wantStart, wantEnd string
		if err := pool.QueryRow(ctx, `SELECT (now() - interval '1 day')::date::text, (now() + interval '59 days')::date::text`).Scan(&wantStart, &wantEnd); err != nil {
			t.Fatal(err)
		}
		if status != want.status || live != 1 || r.starts != wantStart || r.expires != wantEnd || r.used != want.used {
			t.Fatalf("user %d after down: status %q, live rows %d, %+v; want %s, one row %s..%s used %d", user, status, live, r, want.status, wantStart, wantEnd, want.used)
		}
	}
	if err := m.Up(); err != nil {
		t.Fatalf("up from 004 again: %v", err)
	}
}

func TestMigration006DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Steps(5); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO plans (name, price_rub, period_days) VALUES ('p', 1, 30)`,
		`INSERT INTO users (telegram_id, sub_token) VALUES (1, 't1')`,
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status) VALUES ('old-ok', 1, 1, 1, 'succeeded'), ('old-wait', 1, 1, 1, 'pending')`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 006: %v", err)
	}
	var source string
	var paid bool
	var onSale bool
	if err := pool.QueryRow(ctx, `SELECT source, paid_at IS NOT NULL, (SELECT on_sale FROM plans WHERE id = 1) FROM payments WHERE payment_id = 'old-ok'`).Scan(&source, &paid, &onSale); err != nil {
		t.Fatal(err)
	}
	if source != "manual" || !paid || !onSale {
		t.Fatalf("old payment after up 006: source %q, paid %v, plan on sale %v", source, paid, onSale)
	}
	if _, err := pool.Exec(ctx, `UPDATE payments SET status = 'weird' WHERE payment_id = 'old-wait'`); err == nil {
		t.Fatal("unknown payment status accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE payments SET source = 'cash' WHERE payment_id = 'old-wait'`); err == nil {
		t.Fatal("unknown payment source accepted")
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 006: %v", err)
	}
	for _, c := range [][2]string{{"payments", "source"}, {"payments", "confirmation_url"}, {"payments", "paid_at"}, {"plans", "on_sale"}} {
		if columnExists(t, pool, c[0], c[1]) {
			t.Fatalf("down 006 left %s.%s", c[0], c[1])
		}
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 006 again: %v", err)
	}
}

func TestMigration007DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Steps(6); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO plans (name, price_rub, period_days, device_limit) VALUES ('limited', 1, 30, 3)`,
		`INSERT INTO plans (name, price_rub, period_days) VALUES ('open', 1, 30)`,
		`INSERT INTO users (telegram_id, sub_token) VALUES (1, 't1')`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 007: %v", err)
	}
	var limitedHWID, openHWID, trial bool
	var visibility string
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT hwid_required FROM plans WHERE name = 'limited'), (SELECT hwid_required FROM plans WHERE name = 'open'),
		        (SELECT trial FROM plans WHERE name = 'open'), (SELECT visibility FROM plans WHERE name = 'open')`).
		Scan(&limitedHWID, &openHWID, &trial, &visibility); err != nil {
		t.Fatal(err)
	}
	if !limitedHWID || openHWID || trial || visibility != "public" {
		t.Fatalf("after up 007: limited hwid %v, open hwid %v, trial %v, visibility %q", limitedHWID, openHWID, trial, visibility)
	}
	for _, q := range []string{
		`UPDATE plans SET hwid_required = FALSE WHERE name = 'limited'`,
		`UPDATE plans SET trial = TRUE WHERE name = 'open'`,
		`UPDATE plans SET visibility = 'secret' WHERE name = 'open'`,
	} {
		if _, err := pool.Exec(ctx, q); err == nil {
			t.Fatalf("accepted: %s", q)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO plans (name, price_rub, period_days, trial) VALUES ('t1', 0, 3, TRUE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO plans (name, price_rub, period_days, trial) VALUES ('t2', 0, 3, TRUE)`); err == nil {
		t.Fatal("second trial on sale accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO plans (name, price_rub, period_days, trial, on_sale) VALUES ('t3', 0, 3, TRUE, FALSE)`); err != nil {
		t.Fatalf("trial off sale rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO plans (name, price_rub, period_days, visibility) VALUES ('vip', 99, 30, 'private')`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"trials", "trial_devices", "plan_invites", "plan_access", "traffic_daily"} {
		if !tableExists(t, pool, table) {
			t.Fatalf("up 007 did not create %s", table)
		}
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 007: %v", err)
	}
	var vipOnSale, openOnSale bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT on_sale FROM plans WHERE name = 'vip'), (SELECT on_sale FROM plans WHERE name = 'open')`).Scan(&vipOnSale, &openOnSale); err != nil {
		t.Fatal(err)
	}
	if vipOnSale || !openOnSale {
		t.Fatalf("after down 007: private plan on sale %v, public plan on sale %v", vipOnSale, openOnSale)
	}
	for _, c := range []string{"hwid_required", "trial", "visibility"} {
		if columnExists(t, pool, "plans", c) {
			t.Fatalf("down 007 left plans.%s", c)
		}
	}
	for _, table := range []string{"trials", "trial_devices", "plan_invites", "plan_access", "traffic_daily"} {
		if tableExists(t, pool, table) {
			t.Fatalf("down 007 left %s", table)
		}
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 007 again: %v", err)
	}
}

func nodesVersion(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var v int64
	if err := pool.QueryRow(context.Background(), `SELECT users_version FROM nodes WHERE cert_cn = 'n1'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMigration008DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Steps(7); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO nodes (cert_cn, name) VALUES ('n1', 'n1')`,
		`INSERT INTO plans (name, price_rub, period_days, traffic_limit_bytes) VALUES ('p', 1, 30, 100)`,
		`INSERT INTO users (telegram_id, sub_token) VALUES (1, 't1')`,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) VALUES (1, 1, now(), now() + interval '30 days')`,
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status) VALUES ('pay-1', 1, 1, 1, 'succeeded')`,
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status) VALUES ('pay-2', 1, 1, 1, 'mismatch')`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 008: %v", err)
	}
	for _, c := range [][2]string{{"users", "banned_at"}, {"users", "ban_reason"}, {"subscriptions", "traffic_bonus_bytes"},
		{"payments", "period_id"}, {"payments", "note"}, {"payments", "refunded_at"}} {
		if !columnExists(t, pool, c[0], c[1]) {
			t.Fatalf("up 008 did not add %s.%s", c[0], c[1])
		}
	}
	if !tableExists(t, pool, "admin_audit") {
		t.Fatal("up 008 did not create admin_audit")
	}
	for _, q := range []string{
		`UPDATE users SET status = 'weird' WHERE id = 1`,
		`UPDATE subscriptions SET traffic_bonus_bytes = -1 WHERE id = 1`,
		`UPDATE payments SET status = 'lost' WHERE payment_id = 'pay-1'`,
	} {
		if _, err := pool.Exec(ctx, q); err == nil {
			t.Fatalf("accepted: %s", q)
		}
	}
	for _, q := range []string{
		`UPDATE users SET status = 'banned', banned_at = now() WHERE id = 1`,
		`UPDATE payments SET status = 'refunded', refunded_at = now(), period_id = 1 WHERE payment_id = 'pay-1'`,
		`UPDATE payments SET status = 'rejected', note = 'bad amount' WHERE payment_id = 'pay-2'`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	before := nodesVersion(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE plans SET name = 'renamed', on_sale = FALSE, visibility = 'private' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if v := nodesVersion(t, pool); v != before {
		t.Fatalf("renaming a plan bumped nodes: %d -> %d", before, v)
	}
	if _, err := pool.Exec(ctx, `UPDATE plans SET traffic_limit_bytes = traffic_limit_bytes WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if v := nodesVersion(t, pool); v != before {
		t.Fatalf("unchanged limit bumped nodes: %d -> %d", before, v)
	}
	if _, err := pool.Exec(ctx, `UPDATE plans SET traffic_limit_bytes = 200 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	afterLimit := nodesVersion(t, pool)
	if afterLimit == before {
		t.Fatal("changing traffic limit did not bump nodes")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO location_groups (name) VALUES ('g')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE plans SET location_group_id = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	afterGroup := nodesVersion(t, pool)
	if afterGroup == afterLimit {
		t.Fatal("changing plan group did not bump nodes")
	}
	if _, err := pool.Exec(ctx, `UPDATE subscriptions SET traffic_bonus_bytes = 50 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if nodesVersion(t, pool) == afterGroup {
		t.Fatal("traffic bonus did not bump nodes")
	}
	if !tableExists(t, pool, "idx_payments_period") {
		t.Fatal("up 008 did not index payments.period_id")
	}
	beforeDown := nodesVersion(t, pool)
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 008: %v", err)
	}
	if nodesVersion(t, pool) == beforeDown {
		t.Fatal("down 008 did not bump the nodes after dropping the bonuses")
	}
	var status1, status2, userStatus string
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT status FROM payments WHERE payment_id = 'pay-1'), (SELECT status FROM payments WHERE payment_id = 'pay-2'),
		        (SELECT status FROM users WHERE id = 1)`).Scan(&status1, &status2, &userStatus); err != nil {
		t.Fatal(err)
	}
	if status1 != "canceled" || status2 != "canceled" || userStatus != "banned" {
		t.Fatalf("after down 008: payments %q %q, user %q", status1, status2, userStatus)
	}
	for _, c := range [][2]string{{"users", "banned_at"}, {"users", "ban_reason"}, {"subscriptions", "traffic_bonus_bytes"},
		{"payments", "period_id"}, {"payments", "note"}, {"payments", "refunded_at"}} {
		if columnExists(t, pool, c[0], c[1]) {
			t.Fatalf("down 008 left %s.%s", c[0], c[1])
		}
	}
	if tableExists(t, pool, "admin_audit") {
		t.Fatal("down 008 left admin_audit")
	}
	before = nodesVersion(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE plans SET name = 'again' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if nodesVersion(t, pool) == before {
		t.Fatal("down 008 did not restore the plans trigger")
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 008 again: %v", err)
	}
}

func TestMigration009DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Steps(8); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO nodes (cert_cn, name) VALUES ('n1', 'n1')`,
		`INSERT INTO users (telegram_id, sub_token) VALUES (1, 't1')`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 009: %v", err)
	}
	for _, name := range []string{"telegram_profiles", "settings", "idx_traffic_daily_node_day"} {
		if !tableExists(t, pool, name) {
			t.Fatalf("up 009 did not create %s", name)
		}
	}
	var title, support string
	if err := pool.QueryRow(ctx, `SELECT subs_title, subs_support_url FROM settings`).Scan(&title, &support); err != nil {
		t.Fatal(err)
	}
	if title != "Sylph" || support != "" {
		t.Fatalf("default settings = %q %q", title, support)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO settings (id) VALUES (FALSE)`); err == nil {
		t.Fatal("a second settings row was accepted")
	}
	before := nodesVersion(t, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_profiles (user_id, username, first_name) VALUES (1, 'ivan', 'Иван')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE telegram_profiles SET username = 'ivan2' WHERE user_id = 1`); err != nil {
		t.Fatal(err)
	}
	if v := nodesVersion(t, pool); v != before {
		t.Fatalf("a telegram profile bumped nodes: %d -> %d", before, v)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 009: %v", err)
	}
	for _, name := range []string{"telegram_profiles", "settings", "idx_traffic_daily_node_day"} {
		if tableExists(t, pool, name) {
			t.Fatalf("down 009 left %s", name)
		}
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 009 again: %v", err)
	}
}

func TestMigration010DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Steps(9); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO nodes (cert_cn, name) VALUES ('n1', 'n1'), ('n2', 'n2')`); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 010: %v", err)
	}
	for _, name := range []string{"routing_profiles", "node_links", "geo_sources"} {
		if !tableExists(t, pool, name) {
			t.Fatalf("up 010 did not create %s", name)
		}
	}
	for _, c := range [][2]string{{"nodes", "profile_id"}, {"nodes", "geo_status"}, {"nodes", "routing_error"}, {"settings", "geo_generation"}} {
		if !columnExists(t, pool, c[0], c[1]) {
			t.Fatalf("up 010 did not add %s.%s", c[0], c[1])
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO routing_profiles (name, doc) VALUES ('p', '{}')`); err != nil {
		t.Fatal(err)
	}
	before := nodesVersion(t, pool)
	var v1, v2 int64
	if err := pool.QueryRow(ctx, `SELECT users_version FROM nodes WHERE cert_cn = 'n2'`).Scan(&v2); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE nodes SET profile_id = 1 WHERE cert_cn = 'n1'`); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT users_version FROM nodes WHERE cert_cn = 'n1'`).Scan(&v1); err != nil {
		t.Fatal(err)
	}
	var after2 int64
	if err := pool.QueryRow(ctx, `SELECT users_version FROM nodes WHERE cert_cn = 'n2'`).Scan(&after2); err != nil {
		t.Fatal(err)
	}
	if v1 <= before || after2 != v2 {
		t.Fatalf("profile change bumped wrong nodes: n1 %d (before %d), n2 %d -> %d", v1, before, v2, after2)
	}
	for _, q := range []string{
		`INSERT INTO node_links (from_node_id, to_node_id, key_uuid) VALUES (1, 1, 'k')`,
		`INSERT INTO geo_sources (name, kind, url) VALUES ('x', 'other', 'https://x.example.com/a.dat')`,
	} {
		if _, err := pool.Exec(ctx, q); err == nil {
			t.Fatalf("accepted: %s", q)
		}
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 010: %v", err)
	}
	for _, name := range []string{"routing_profiles", "node_links", "geo_sources"} {
		if tableExists(t, pool, name) {
			t.Fatalf("down 010 left %s", name)
		}
	}
	if columnExists(t, pool, "nodes", "profile_id") || columnExists(t, pool, "settings", "geo_generation") {
		t.Fatal("down 010 left columns")
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 010 again: %v", err)
	}
}

func TestMigration011DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Steps(10); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO nodes (cert_cn, name) VALUES ('n1', 'n1')`); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 011: %v", err)
	}
	for _, name := range []string{"services", "health_marks"} {
		if !tableExists(t, pool, name) {
			t.Fatalf("up 011 did not create %s", name)
		}
	}
	if !columnExists(t, pool, "nodes", "cert_not_after") {
		t.Fatal("up 011 did not add nodes.cert_not_after")
	}
	before := nodesVersion(t, pool)
	for _, q := range []string{
		`UPDATE nodes SET cert_not_after = now() + interval '1 year' WHERE cert_cn = 'n1'`,
		`INSERT INTO services (cert_cn, role, last_seen) VALUES ('bot', 'bot', now())`,
		`INSERT INTO health_marks (name, ok_at) VALUES ('cron_expire', now())`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if v := nodesVersion(t, pool); v != before {
		t.Fatalf("monitoring writes bumped nodes: %d -> %d", before, v)
	}
	for _, q := range []string{
		`INSERT INTO services (cert_cn, role, last_seen) VALUES ('x', 'admin', now())`,
		`INSERT INTO health_marks (name) VALUES ('unknown')`,
	} {
		if _, err := pool.Exec(ctx, q); err == nil {
			t.Fatalf("accepted: %s", q)
		}
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 011: %v", err)
	}
	for _, name := range []string{"services", "health_marks"} {
		if tableExists(t, pool, name) {
			t.Fatalf("down 011 left %s", name)
		}
	}
	if columnExists(t, pool, "nodes", "cert_not_after") {
		t.Fatal("down 011 left nodes.cert_not_after")
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 011 again: %v", err)
	}
}

func TestMigration012DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Steps(11); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE settings SET subs_title = 'Нора'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (telegram_id, sub_token) VALUES (1, 'tok')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO nodes (cert_cn, name) VALUES ('n1', 'n1')`); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 012: %v", err)
	}
	if !tableExists(t, pool, "notifications_sent") || !columnExists(t, pool, "subscriptions", "closed_manually") {
		t.Fatal("up 012 did not create notifications_sent or subscriptions.closed_manually")
	}
	var traffic, expiry, trial bool
	var title string
	if err := pool.QueryRow(ctx, `SELECT notify_traffic, notify_expiry, notify_trial, subs_title FROM settings`).Scan(&traffic, &expiry, &trial, &title); err != nil {
		t.Fatal(err)
	}
	if !traffic || !expiry || !trial || title != "Нора" {
		t.Fatalf("settings after 012: %v %v %v %q", traffic, expiry, trial, title)
	}
	before := nodesVersion(t, pool)
	if _, err := pool.Exec(ctx,
		`INSERT INTO notifications_sent (user_id, kind, ref, paid_until, outcome) SELECT id, 'expired', 'r', now(), 'sent' FROM users
		 UNION ALL SELECT id, 'expired', 'rf', now(), 'failed' FROM users`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE settings SET notify_trial = FALSE`); err != nil {
		t.Fatal(err)
	}
	if v := nodesVersion(t, pool); v != before {
		t.Fatalf("notification writes bumped nodes: %d -> %d", before, v)
	}
	for _, q := range []string{
		`INSERT INTO notifications_sent (user_id, kind, ref, paid_until, outcome) SELECT id, 'traffic_50', 'r2', now(), 'sent' FROM users`,
		`INSERT INTO notifications_sent (user_id, kind, ref, paid_until, outcome) SELECT id, 'expired', 'r3', now(), 'maybe' FROM users`,
		`INSERT INTO notifications_sent (user_id, kind, ref, outcome) SELECT id, 'expired', 'r4', 'sent' FROM users`,
		`INSERT INTO notifications_sent (user_id, kind, ref, period_id, paid_until, outcome) SELECT id, 'expired', 'r5', 1, now(), 'sent' FROM users`,
	} {
		if _, err := pool.Exec(ctx, q); err == nil {
			t.Fatalf("accepted: %s", q)
		}
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 012: %v", err)
	}
	if tableExists(t, pool, "notifications_sent") {
		t.Fatal("down 012 left notifications_sent")
	}
	for _, c := range []string{"notify_traffic", "notify_expiry", "notify_trial"} {
		if columnExists(t, pool, "settings", c) {
			t.Fatalf("down 012 left settings.%s", c)
		}
	}
	if columnExists(t, pool, "subscriptions", "closed_manually") {
		t.Fatal("down 012 left subscriptions.closed_manually")
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 012 again: %v", err)
	}
}
