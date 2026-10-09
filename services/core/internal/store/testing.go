package store

import (
	"context"
	"os"
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
