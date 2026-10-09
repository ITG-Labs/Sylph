package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPool(t *testing.T) *pgxpool.Pool {
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

func TestPoolWithSchema(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("SYLPH_TEST_DB")
	if url == "" {
		t.Skip("SYLPH_TEST_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dsn := url
	if strings.Contains(dsn, "?") {
		dsn += "&search_path=" + schema
	} else {
		dsn += "?search_path=" + schema
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
