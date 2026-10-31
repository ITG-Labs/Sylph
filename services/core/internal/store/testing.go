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

const (
	TestSharedGroup = "test-shared"
	TestMainPool    = "test-main"
)

func ShareForTests(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO location_groups (name) VALUES ($1) RETURNING id`, TestSharedGroup).Scan(&id); err != nil {
		return 0, err
	}
	_, err := pool.Exec(ctx, `
CREATE FUNCTION test_share_node() RETURNS trigger AS $$
BEGIN
    INSERT INTO group_nodes (group_id, node_id) SELECT g.id, NEW.id FROM location_groups g WHERE g.name = '`+TestSharedGroup+`';
    RETURN NULL;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER test_share_node AFTER INSERT ON nodes FOR EACH ROW EXECUTE FUNCTION test_share_node();
CREATE FUNCTION test_fill_main_pools() RETURNS trigger AS $$
BEGIN
    INSERT INTO plan_pool_inbounds (plan_id, pool_id, inbound_id)
    SELECT pp.plan_id, pp.id, NEW.id FROM plan_pools pp WHERE pp.name = '`+TestMainPool+`'
    ON CONFLICT DO NOTHING;
    RETURN NULL;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER test_fill_main_pools AFTER INSERT ON node_inbounds FOR EACH ROW EXECUTE FUNCTION test_fill_main_pools();`)
	return id, err
}

func LimitAllForTests(ctx context.Context, pool *pgxpool.Pool, planID, limit int64) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx,
		`WITH p AS (INSERT INTO plan_pools (plan_id, position, name, traffic_limit_bytes) VALUES ($1, -1, $2, $3) RETURNING id),
		 m AS (INSERT INTO plan_pool_inbounds (plan_id, pool_id, inbound_id)
		       SELECT $1, p.id, i.id FROM p, node_inbounds i ON CONFLICT DO NOTHING)
		 SELECT id FROM p`, planID, TestMainPool, limit).Scan(&id)
	return id, err
}
