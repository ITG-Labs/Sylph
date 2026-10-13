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
	if count != 16 {
		t.Fatalf("tables = %d, want 16", count)
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
