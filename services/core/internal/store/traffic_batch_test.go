package store

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

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

func TestPoolCountersScaleLinearly(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	plan := mkLimitedPlan(t, s, 30, 1<<40)
	pool := mainPool(t, s, plan.ID)
	const n = 10000
	for _, q := range []string{
		`INSERT INTO users (sub_token, email, status) SELECT 'scale' || i, '', 'active' FROM generate_series(1, 10000) i`,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) SELECT id, $1, now() - interval '1 day', now() + interval '29 days' FROM users WHERE sub_token LIKE 'scale%'`,
		`INSERT INTO period_pool_usage (period_id, pool_id, used_bytes) SELECT id, $2, 1000 FROM subscriptions WHERE plan_id = $1 AND id % 2 = 0`,
	} {
		args := []any{plan.ID, pool}
		if !strings.Contains(q, "$2") {
			args = args[:1]
		}
		if !strings.Contains(q, "$1") {
			args = nil
		}
		if _, err := s.pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM subscriptions WHERE plan_id = $1 ORDER BY id`, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	periods, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil || len(periods) != n {
		t.Fatalf("periods %d, %v", len(periods), err)
	}
	var adds poolAdds
	for _, p := range periods {
		adds.add(p, &pool, 100)
		adds.add(p, nil, 50)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	start := time.Now()
	got, err := adds.apply(ctx, tx)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d pooled and %d main counters in %v", n, n, took)
	if took > 5*time.Second {
		t.Fatalf("counters took %v", took)
	}
	for _, p := range periods {
		before := int64(0)
		if p%2 == 0 {
			before = 1000
		}
		if r := got[poolKey{p, pool}]; r.before != before || r.used != before+100 {
			t.Fatalf("period %d pool: %+v", p, r)
		}
		if r := got[poolKey{p, 0}]; r.before != 0 || r.used != 50 {
			t.Fatalf("period %d main: %+v", p, r)
		}
	}
}

func lateBatchFixture(t *testing.T, s *Store) (node *model.Node, key string, userID, oldPeriod, newPeriod int64) {
	t.Helper()
	ctx := context.Background()
	node = deltaNode(t, s)
	u, key := userWithKey(t, s)
	plan := mkPlan(t, s)
	res := pay(t, s, u.ID, plan.ID, time.Now())
	if _, err := s.pool.Exec(ctx,
		`UPDATE subscriptions SET starts_at = now() - interval '10 hours', expires_at = now() - interval '2 hours' WHERE id = $1`, res.PeriodID); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) VALUES ($1, $2, now() - interval '2 hours', now() + interval '28 days') RETURNING id`,
		u.ID, plan.ID).Scan(&newPeriod); err != nil {
		t.Fatal(err)
	}
	return node, key, u.ID, res.PeriodID, newPeriod
}

func periodUsed(t *testing.T, s *Store, periodID int64) int64 {
	t.Helper()
	var used int64
	if err := s.pool.QueryRow(context.Background(), `SELECT traffic_used_bytes FROM subscriptions WHERE id = $1`, periodID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	return used
}

func TestLateBatchIsChargedToThePeriodItWasSpentIn(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	node, key, userID, oldPeriod, newPeriod := lateBatchFixture(t, s)
	spent := time.Now().Add(-5 * time.Hour)
	delta := []model.TrafficDelta{{KeyUUID: key, Inbound: "main", BytesUp: 600, BytesDown: 400}}
	if _, err := s.RecordDeltaBatchFrom(ctx, node.ID, "late-1", spent, delta); err != nil {
		t.Fatal(err)
	}
	if got := periodUsed(t, s, oldPeriod); got != 1000 {
		t.Fatalf("old period used %d, want 1000", got)
	}
	if got := periodUsed(t, s, newPeriod); got != 0 {
		t.Fatalf("new period used %d, want 0", got)
	}
	var rows int
	var day string
	var wantDay string
	if err := s.pool.QueryRow(ctx, `SELECT count(*), COALESCE(min(day::text), '') FROM traffic_daily WHERE user_id = $1 AND period_id = $2`, userID, oldPeriod).Scan(&rows, &day); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT (($1::timestamptz) AT TIME ZONE $2)::date::text`, spent, s.statsTZ).Scan(&wantDay); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || day != wantDay {
		t.Fatalf("daily rows of the old period = %d on %q, want 1 on %q", rows, day, wantDay)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM traffic_daily WHERE user_id = $1 AND period_id = $2`, userID, newPeriod).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("daily rows of the new period = %d, %v", rows, err)
	}
}

func TestBatchWithoutOrWithBadSpentTimeIsChargedToTheCurrentPeriod(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	node, key, _, oldPeriod, newPeriod := lateBatchFixture(t, s)
	delta := []model.TrafficDelta{{KeyUUID: key, Inbound: "main", BytesUp: 10}}
	for i, from := range []time.Time{{}, time.Now().Add(48 * time.Hour), time.Now().AddDate(-3, 0, 0), time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if _, err := s.RecordDeltaBatchFrom(ctx, node.ID, "cur-"+string(rune('a'+i)), from, delta); err != nil {
			t.Fatal(err)
		}
	}
	if got := periodUsed(t, s, newPeriod); got != 40 {
		t.Fatalf("current period used %d, want 40", got)
	}
	if got := periodUsed(t, s, oldPeriod); got != 0 {
		t.Fatalf("old period used %d, want 0", got)
	}
}
