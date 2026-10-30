package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func seedBigBase(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO location_groups (name) SELECT 'g' || i FROM generate_series(1, 10) i`,
		`INSERT INTO nodes (cert_cn, name, address) SELECT 'n' || i, 'n' || i, 'n' || i || '.example.com' FROM generate_series(1, 40) i`,
		`INSERT INTO node_inbounds (node_id, tag, port, protocol, transport, security)
		 SELECT n.id, t, 443, 'vless', 'tcp', 'reality' FROM nodes n, (VALUES ('main'), ('cdn')) v(t)`,
		`INSERT INTO group_nodes (group_id, node_id) SELECT g.id, n.id FROM location_groups g JOIN nodes n ON n.id % 10 = g.id % 10 WHERE n.id <= 30`,
		`INSERT INTO group_inbounds (group_id, inbound_id)
		 SELECT g.id, i.id FROM location_groups g JOIN node_inbounds i ON i.node_id > 30 AND i.tag = 'cdn' AND i.node_id % 10 = g.id % 10`,
		`INSERT INTO plans (name, price_rub, period_days, traffic_limit_bytes, location_group_id)
		 SELECT 'p' || i, 100, 30, 1099511627776, CASE WHEN i % 4 = 0 THEN NULL ELSE (SELECT id FROM location_groups ORDER BY id OFFSET (i % 10) LIMIT 1) END
		 FROM generate_series(1, 20) i`,
		`INSERT INTO plan_pools (plan_id, position, node_id, traffic_limit_bytes)
		 SELECT p.id, 0, (SELECT n.id FROM group_nodes gn JOIN nodes n ON n.id = gn.node_id WHERE gn.group_id = p.location_group_id ORDER BY n.id LIMIT 1), 107374182400
		 FROM plans p WHERE p.location_group_id IS NOT NULL AND p.id % 2 = 0`,
		`INSERT INTO users (telegram_id, sub_token, email) SELECT i, 'tok' || i, '' FROM generate_series(1, 20000) i`,
		`INSERT INTO user_keys (user_id, uuid) SELECT id, md5(id::text) FROM users`,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at, traffic_used_bytes)
		 SELECT u.id, (SELECT id FROM plans ORDER BY id OFFSET (u.id % 20) LIMIT 1), now() - interval '1 day', now() + interval '29 days',
		        CASE WHEN u.id % 7 = 0 THEN 900000000000 ELSE u.id * 1000 END
		 FROM users u`,
		`INSERT INTO period_pool_usage (period_id, pool_id, used_bytes)
		 SELECT s.id, pp.id, CASE WHEN s.id % 5 = 0 THEN 107374182400 ELSE 90000000000 END
		 FROM subscriptions s JOIN plan_pools pp ON pp.plan_id = s.plan_id WHERE s.id % 3 = 0`,
		`ANALYZE`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func timed(t *testing.T, name string, budget time.Duration, f func() error) {
	t.Helper()
	start := time.Now()
	if err := f(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	took := time.Since(start)
	t.Logf("%s: %v", name, took)
	if took > budget {
		t.Errorf("%s took %v, budget %v", name, took, budget)
	}
}

func TestPoolQueriesStayFastOnABigBase(t *testing.T) {
	if os.Getenv("SYLPH_TEST_BIG") == "" {
		t.Skip("SYLPH_TEST_BIG not set")
	}
	s := setupStore(t)
	ctx := context.Background()
	seedBigBase(t, s)
	timed(t, "notify candidates", 2*time.Second, func() error {
		_, err := s.NotifyCandidates(ctx, time.Now())
		return err
	})
	for _, node := range []int64{5, 35} {
		timed(t, fmt.Sprintf("node %d keys", node), time.Second, func() error {
			_, err := s.ListNodeKeys(ctx, node)
			return err
		})
	}
	timed(t, "user list", time.Second, func() error {
		_, err := s.ListUsers(ctx, UserFilter{Limit: 100})
		return err
	})
	var deltas []model.TrafficDelta
	for range 5000 {
		deltas = append(deltas, model.TrafficDelta{Inbound: "main", BytesDown: 1000})
	}
	rows, err := s.pool.Query(ctx, `SELECT uuid FROM user_keys ORDER BY user_id LIMIT 5000`)
	if err != nil {
		t.Fatal(err)
	}
	i := 0
	for rows.Next() {
		if err := rows.Scan(&deltas[i].KeyUUID); err != nil {
			t.Fatal(err)
		}
		i++
	}
	rows.Close()
	timed(t, "5000 deltas", 10*time.Second, func() error {
		_, err := s.RecordDeltaBatch(ctx, 5, "big-1", deltas)
		return err
	})
}
