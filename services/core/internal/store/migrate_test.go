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
	if count != 9 {
		t.Fatalf("tables = %d, want 9", count)
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
