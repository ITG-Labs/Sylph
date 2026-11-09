package store

import (
	"context"
	"testing"
	"time"
)

func TestCleanupDeltasBudgetDoesNotStarveOtherSteps(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	deltaNode(t, s)
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO traffic_deltas (node_id, key_uuid, bytes_up, bytes_down, reported_at)
		 SELECT (SELECT min(id) FROM nodes), 'k', 1, 1, now() - interval '40 days' FROM generate_series(1, 20000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO traffic_batches (node_id, batch_id, received_at) SELECT min(id), 'b-old', now() - interval '40 days' FROM nodes`); err != nil {
		t.Fatal(err)
	}
	prev := cleanupStepBudget
	cleanupStepBudget = 300 * time.Millisecond
	t.Cleanup(func() { cleanupStepBudget = prev })
	res, err := s.CleanupHistory(ctx, time.Now().Add(-30*day), 1)
	if err != nil {
		t.Fatalf("a spent budget is not an error: %v", err)
	}
	if !res.Partial || res.Deltas == 0 || res.Deltas >= 20000 {
		t.Fatalf("cleanup = %+v, want a partial pass over the deltas", res)
	}
	if res.Batches != 1 {
		t.Fatalf("batches = %d: the cheap steps must not wait for the deltas", res.Batches)
	}
	cleanupStepBudget = time.Minute
	rest, err := s.CleanupHistory(ctx, time.Now().Add(-30*day), 10000)
	if err != nil || rest.Partial || res.Deltas+rest.Deltas != 20000 {
		t.Fatalf("second pass = %+v, %v", rest, err)
	}
}

func TestCleanupStepAlwaysDeletesAtLeastOneChunk(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	deltaNode(t, s)
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO traffic_deltas (node_id, key_uuid, bytes_up, bytes_down, reported_at)
		 SELECT (SELECT min(id) FROM nodes), 'k', 1, 1, now() - interval '40 days' FROM generate_series(1, 1000)`); err != nil {
		t.Fatal(err)
	}
	prev := cleanupStepBudget
	cleanupStepBudget = time.Nanosecond
	t.Cleanup(func() { cleanupStepBudget = prev })
	res, err := s.CleanupHistory(ctx, time.Now().Add(-30*day), 100)
	if err != nil || !res.Partial || res.Deltas != 100 {
		t.Fatalf("cleanup = %+v, %v; a spent budget must still let one whole chunk through", res, err)
	}
}

func TestCleanupDailyAndIPsRunInBudgetedChunks(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := deltaNode(t, s)
	u, _ := userWithKey(t, s)
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO traffic_daily (user_id, node_id, day, period_id, inbound_tag, bytes_up)
		 SELECT $1, $2, current_date - 400 - g, 0, '', 1 FROM generate_series(1, 50) g`, u.ID, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO ip_history (user_id, node_id, ip, first_seen, last_seen)
		 SELECT $1, $2, '10.0.0.' || g, now() - interval '60 days', now() - interval '50 days' FROM generate_series(1, 50) g`, u.ID, n.ID); err != nil {
		t.Fatal(err)
	}
	prev := cleanupStepBudget
	cleanupStepBudget = time.Nanosecond
	t.Cleanup(func() { cleanupStepBudget = prev })
	daily, partial, err := s.CleanupDailyStats(ctx, 365, 10)
	if err != nil || !partial || daily != 10 {
		t.Fatalf("daily = %d partial %v, %v", daily, partial, err)
	}
	ips, partial, err := s.CleanupOnline(ctx, time.Now().AddDate(0, 0, -30), 10)
	if err != nil || !partial || ips != 10 {
		t.Fatalf("ips = %d partial %v, %v", ips, partial, err)
	}
	cleanupStepBudget = time.Minute
	daily, partial, err = s.CleanupDailyStats(ctx, 365, 10)
	if err != nil || partial || daily != 40 {
		t.Fatalf("daily rest = %d partial %v, %v", daily, partial, err)
	}
	ips, partial, err = s.CleanupOnline(ctx, time.Now().AddDate(0, 0, -30), 10)
	if err != nil || partial || ips != 40 {
		t.Fatalf("ips rest = %d partial %v, %v", ips, partial, err)
	}
}
