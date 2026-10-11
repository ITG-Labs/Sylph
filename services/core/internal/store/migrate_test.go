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
	if count != 11 {
		t.Fatalf("tables = %d, want 11", count)
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
	if err := m.Steps(-2); err != nil {
		t.Fatalf("down 004 and 003: %v", err)
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
	if err := m.Steps(2); err != nil {
		t.Fatalf("up 003 and 004 again: %v", err)
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
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 004: %v", err)
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
	if err := m.Steps(1); err != nil {
		t.Fatalf("up 004 again: %v", err)
	}
	if !tableExists(t, pool, "node_inbounds") || columnExists(t, pool, "nodes", "config") {
		t.Fatal("up 004 again did not reshape nodes")
	}
}
