package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestDeltaBatchForManyUsersKeepsPerUserAccounting(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	node := deltaNode(t, s)
	limited := mkLimitedPlan(t, s, 30, 1000)
	free := &model.Plan{Name: model.NewToken(), PriceRub: 100, PeriodDays: 30, LocationGroupID: sharedGroup(t, s)}
	if err := s.CreatePlan(ctx, free); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	const users = 300
	var deltas []model.TrafficDelta
	var wantExhausted []int64
	periodOf := map[int64]int64{}
	for i := range users {
		u, key := userWithKey(t, s)
		plan := free.ID
		if i%2 == 0 {
			plan = limited.ID
		}
		res := pay(t, s, u.ID, plan, now)
		periodOf[u.ID] = res.PeriodID
		if i%4 == 0 {
			setUsed(t, s, res.PeriodID, 900)
			wantExhausted = append(wantExhausted, u.ID)
		}
		deltas = append(deltas, model.TrafficDelta{KeyUUID: key, Inbound: "main", BytesUp: 100, BytesDown: 100})
	}
	for range 3 {
		deltas = append(deltas, model.TrafficDelta{KeyUUID: model.NewUUID(), Inbound: "main", BytesUp: 7, BytesDown: 7})
	}
	start := time.Now()
	res := record(t, s, node.ID, "many-users", deltas...)
	t.Logf("%d deltas recorded in %v", len(deltas), time.Since(start))

	var got []int64
	for _, e := range res.Exhausted {
		if e.Limit != 1000 || e.Used != 1100 {
			t.Errorf("exhaustion %+v", e)
		}
		got = append(got, e.UserID)
	}
	if !slices.Equal(got, wantExhausted) {
		t.Fatalf("exhausted %v, want %v", got, wantExhausted)
	}
	var orphans, rows int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE user_id IS NULL), count(*) FROM traffic_deltas WHERE node_id = $1`, node.ID).Scan(&orphans, &rows); err != nil {
		t.Fatal(err)
	}
	if orphans != 3 || rows != users+3 {
		t.Fatalf("deltas stored: %d of %d without owner", orphans, rows)
	}
	var daily, dailyBytes int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*), sum(bytes_up + bytes_down) FROM traffic_daily WHERE node_id = $1 AND inbound_tag = 'main'`, node.ID).Scan(&daily, &dailyBytes); err != nil {
		t.Fatal(err)
	}
	if daily != users || dailyBytes != users*200 {
		t.Fatalf("daily rows %d, bytes %d", daily, dailyBytes)
	}
	pool := mainPool(t, s, limited.ID)
	for uid, pid := range periodOf {
		var used int64
		err := s.pool.QueryRow(ctx,
			`SELECT COALESCE((SELECT used_bytes FROM period_pool_usage WHERE period_id = $1 AND pool_id = $2),
			                 (SELECT traffic_used_bytes FROM subscriptions WHERE id = $1))`, pid, pool).Scan(&used)
		if err != nil {
			t.Fatal(err)
		}
		want := int64(200)
		if slices.Contains(wantExhausted, uid) {
			want = 1100
		}
		if used != want {
			t.Fatalf("user %d period %d used %d, want %d", uid, pid, used, want)
		}
	}
	again := record(t, s, node.ID, "many-users", deltas...)
	if !again.Duplicate {
		t.Fatal("same batch id must be ignored")
	}
}
