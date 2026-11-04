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
