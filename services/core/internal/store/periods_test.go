package store

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

const day = 24 * time.Hour

func mkLimitedPlan(t *testing.T, s *Store, days int, limit int64) *model.Plan {
	t.Helper()
	p := &model.Plan{Name: model.NewToken(), PriceRub: 100, PeriodDays: days, TrafficLimitBytes: &limit, LocationGroupID: testGroup(t, s)}
	if err := s.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func userPeriods(t *testing.T, s *Store, userID int64) []model.Subscription {
	t.Helper()
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, user_id, plan_id, starts_at, expires_at, canceled, traffic_used_bytes, created_at
		 FROM subscriptions WHERE user_id = $1 ORDER BY starts_at, id`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []model.Subscription
	for rows.Next() {
		var p model.Subscription
		if err := rows.Scan(&p.ID, &p.UserID, &p.PlanID, &p.StartsAt, &p.ExpiresAt, &p.Canceled, &p.TrafficUsedBytes, &p.CreatedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func pay(t *testing.T, s *Store, userID, planID int64, now time.Time) PaymentResult {
	t.Helper()
	res, err := s.ApplyPayment(context.Background(), userID, planID, now)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func sameTime(a, b time.Time) bool {
	return a.Sub(b).Abs() < time.Millisecond
}

func setUsed(t *testing.T, s *Store, periodID, used int64) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `UPDATE subscriptions SET traffic_used_bytes = $2 WHERE id = $1`, periodID, used); err != nil {
		t.Fatal(err)
	}
}

func TestFirstPaymentStartsNow(t *testing.T) {
	s := setupStore(t)
	u, p := mkUser(t, s), mkPlan(t, s)
	now := time.Now().UTC()
	res := pay(t, s, u.ID, p.ID, now)
	if !res.StartsNow {
		t.Fatal("first payment must start now")
	}
	ps := userPeriods(t, s, u.ID)
	if len(ps) != 1 || !sameTime(ps[0].StartsAt, now) || !sameTime(ps[0].ExpiresAt, now.AddDate(0, 0, 30)) || ps[0].ID != res.PeriodID {
		t.Fatalf("periods = %+v", ps)
	}
}

func TestSamePlanQueuesAfterTheLastPeriod(t *testing.T) {
	s := setupStore(t)
	u, p := mkUser(t, s), mkPlan(t, s)
	now := time.Now().UTC()
	pay(t, s, u.ID, p.ID, now)
	second := pay(t, s, u.ID, p.ID, now.Add(time.Hour))
	third := pay(t, s, u.ID, p.ID, now.Add(2*time.Hour))
	if second.StartsNow || third.StartsNow {
		t.Fatal("same plan must queue")
	}
	ps := userPeriods(t, s, u.ID)
	if len(ps) != 3 {
		t.Fatalf("periods = %+v", ps)
	}
	for i := 1; i < 3; i++ {
		if !sameTime(ps[i].StartsAt, ps[i-1].ExpiresAt) || !sameTime(ps[i].ExpiresAt, ps[i].StartsAt.AddDate(0, 0, 30)) || ps[i].Canceled {
			t.Fatalf("period %d not queued back to back: %+v", i, ps)
		}
	}
	active, err := s.GetActiveSubscription(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != ps[0].ID || active.PaidUntil == nil || !sameTime(*active.PaidUntil, ps[2].ExpiresAt) {
		t.Fatalf("active = %+v, want first period paid until %v", active, ps[2].ExpiresAt)
	}
}

func TestOtherPlanStartsNowBurnsTheRestAndShiftsTheQueue(t *testing.T) {
	s := setupStore(t)
	u, economy, premium := mkUser(t, s), mkPlan(t, s), mkPlan(t, s)
	now := time.Now().UTC()
	pay(t, s, u.ID, economy.ID, now.Add(-10*day))
	pay(t, s, u.ID, economy.ID, now.Add(-9*day))
	pay(t, s, u.ID, economy.ID, now.Add(-8*day))
	res := pay(t, s, u.ID, premium.ID, now)
	if !res.StartsNow {
		t.Fatal("other plan must start now")
	}
	ps := userPeriods(t, s, u.ID)
	if len(ps) != 4 {
		t.Fatalf("periods = %+v", ps)
	}
	old, fresh, queued, last := ps[0], ps[1], ps[2], ps[3]
	if !sameTime(last.StartsAt, queued.ExpiresAt) || last.ExpiresAt.Sub(last.StartsAt) != 30*day {
		t.Fatalf("second prepaid period not shifted behind the first: %+v", ps)
	}
	if !old.Canceled || !sameTime(old.ExpiresAt, now) {
		t.Fatalf("old period not closed now: %+v", old)
	}
	if fresh.ID != res.PeriodID || fresh.PlanID != premium.ID || !sameTime(fresh.StartsAt, now) || !sameTime(fresh.ExpiresAt, now.AddDate(0, 0, 30)) {
		t.Fatalf("new period = %+v", fresh)
	}
	if queued.PlanID != economy.ID || queued.Canceled || !sameTime(queued.StartsAt, fresh.ExpiresAt) || queued.ExpiresAt.Sub(queued.StartsAt) != 30*day {
		t.Fatalf("prepaid period lost or not shifted: %+v", queued)
	}
	active, _ := s.GetActiveSubscription(context.Background(), u.ID)
	if active.ID != fresh.ID {
		t.Fatalf("active = %d, want %d", active.ID, fresh.ID)
	}
}

func TestPaymentForExhaustedPeriodStartsNow(t *testing.T) {
	s := setupStore(t)
	u, p := mkUser(t, s), mkLimitedPlan(t, s, 30, 1000)
	now := time.Now().UTC()
	first := pay(t, s, u.ID, p.ID, now.Add(-5*day))
	queued := pay(t, s, u.ID, p.ID, now.Add(-4*day))
	setUsed(t, s, first.PeriodID, 1000)
	res := pay(t, s, u.ID, p.ID, now)
	if !res.StartsNow {
		t.Fatal("payment while exhausted must start now")
	}
	ps := userPeriods(t, s, u.ID)
	if len(ps) != 3 || ps[0].ID != first.PeriodID || !ps[0].Canceled || !sameTime(ps[0].ExpiresAt, now) {
		t.Fatalf("exhausted period not closed: %+v", ps)
	}
	if ps[1].ID != res.PeriodID || !sameTime(ps[1].StartsAt, now) || ps[1].TrafficUsedBytes != 0 {
		t.Fatalf("fresh period = %+v", ps[1])
	}
	if ps[2].ID != queued.PeriodID || !sameTime(ps[2].StartsAt, ps[1].ExpiresAt) {
		t.Fatalf("queued period not shifted after the fresh one: %+v", ps)
	}
}

func TestBelowLimitSamePlanStillQueues(t *testing.T) {
	s := setupStore(t)
	u, p := mkUser(t, s), mkLimitedPlan(t, s, 30, 1000)
	now := time.Now().UTC()
	first := pay(t, s, u.ID, p.ID, now)
	setUsed(t, s, first.PeriodID, 999)
	if pay(t, s, u.ID, p.ID, now.Add(time.Minute)).StartsNow {
		t.Fatal("period one byte short of the limit is not exhausted")
	}
}

func TestExpiredPeriodsDoNotBlockAFreshStart(t *testing.T) {
	s := setupStore(t)
	u, p := mkUser(t, s), mkPlan(t, s)
	now := time.Now().UTC()
	pay(t, s, u.ID, p.ID, now.Add(-40*day))
	res := pay(t, s, u.ID, p.ID, now)
	if !res.StartsNow {
		t.Fatal("payment after the last period ended must start now")
	}
	active, err := s.GetActiveSubscription(context.Background(), u.ID)
	if err != nil || active.ID != res.PeriodID {
		t.Fatalf("active = %+v, %v", active, err)
	}
}

func deltaNode(t *testing.T, s *Store) *model.Node {
	t.Helper()
	n := &model.Node{CertCN: "node-" + model.NewToken()[:8], Name: "n"}
	if err := s.UpsertNode(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	mkInbound(t, s, n.ID, "main")
	return n
}

func userWithKey(t *testing.T, s *Store) (*model.User, string) {
	t.Helper()
	u := mkUser(t, s)
	k := model.NewUUID()
	if err := s.AddUserKey(context.Background(), u.ID, k); err != nil {
		t.Fatal(err)
	}
	return u, k
}

func record(t *testing.T, s *Store, nodeID int64, batch string, deltas ...model.TrafficDelta) BatchResult {
	t.Helper()
	res, err := s.RecordDeltaBatch(context.Background(), nodeID, batch, deltas)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDeltasCountIntoTheActivePeriodOnly(t *testing.T) {
	s := setupStore(t)
	u, k := userWithKey(t, s)
	p := mkPlan(t, s)
	now := time.Now().UTC()
	pay(t, s, u.ID, p.ID, now)
	pay(t, s, u.ID, p.ID, now)
	n := deltaNode(t, s)
	record(t, s, n.ID, "b-1", model.TrafficDelta{KeyUUID: k, BytesUp: 100, BytesDown: 200}, model.TrafficDelta{KeyUUID: k, BytesUp: 300, BytesDown: 400})
	if res := record(t, s, n.ID, "b-1", model.TrafficDelta{KeyUUID: k, BytesUp: 100, BytesDown: 200}); !res.Duplicate {
		t.Fatal("repeated batch not flagged")
	}
	ps := userPeriods(t, s, u.ID)
	if ps[0].TrafficUsedBytes != 1000 || ps[1].TrafficUsedBytes != 0 {
		t.Fatalf("counters = %d / %d, want 1000 / 0", ps[0].TrafficUsedBytes, ps[1].TrafficUsedBytes)
	}
	var history, owned int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE user_id = $1) FROM traffic_deltas`, u.ID).Scan(&history, &owned); err != nil {
		t.Fatal(err)
	}
	if history != 2 || owned != 2 {
		t.Fatalf("history rows = %d, owned = %d", history, owned)
	}
}

func TestDeltasWithoutActivePeriodOrOwnerOnlyGoToHistory(t *testing.T) {
	s := setupStore(t)
	u, k := userWithKey(t, s)
	n := deltaNode(t, s)
	res := record(t, s, n.ID, "b-1", model.TrafficDelta{KeyUUID: k, BytesUp: 5}, model.TrafficDelta{KeyUUID: model.NewUUID(), BytesUp: 7})
	if res.Duplicate || len(res.Exhausted) != 0 {
		t.Fatalf("res = %+v", res)
	}
	var rows int
	var orphan int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE user_id IS NULL) FROM traffic_deltas`).Scan(&rows, &orphan); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || orphan != 1 {
		t.Fatalf("rows = %d, orphan = %d", rows, orphan)
	}
	if len(userPeriods(t, s, u.ID)) != 0 {
		t.Fatal("a period appeared out of nowhere")
	}
}

func TestRotatedKeyStillCountsAndLeavesTheNodes(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, oldKey := userWithKey(t, s)
	p := mkPlan(t, s)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	n := deltaNode(t, s)
	record(t, s, n.ID, "b-1", model.TrafficDelta{KeyUUID: oldKey, BytesUp: 400})
	fresh, err := s.RotateUserCredentials(ctx, u.ID, model.NewToken())
	if err != nil {
		t.Fatal(err)
	}
	record(t, s, n.ID, "b-2", model.TrafficDelta{KeyUUID: oldKey, BytesUp: 100}, model.TrafficDelta{KeyUUID: fresh, BytesDown: 500})
	if got := userPeriods(t, s, u.ID)[0].TrafficUsedBytes; got != 1000 {
		t.Fatalf("used after rotation = %d, want 1000", got)
	}
	keys, err := s.GetUserKeys(ctx, u.ID)
	if err != nil || len(keys) != 1 || keys[0].UUID != fresh {
		t.Fatalf("keys = %+v, %v", keys, err)
	}
	onNode, err := s.ListNodeUserKeys(ctx, n.ID)
	if err != nil || len(onNode) != 1 || onNode[0] != fresh {
		t.Fatalf("node keys = %v, %v", onNode, err)
	}
}

func TestCrossingTheLimitBumpsNodesOnce(t *testing.T) {
	s := setupStore(t)
	u, k := userWithKey(t, s)
	p := mkLimitedPlan(t, s, 30, 1000)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	n := deltaNode(t, s)
	v0 := nodeVersion(t, s, n.CertCN)
	if res := record(t, s, n.ID, "b-1", model.TrafficDelta{KeyUUID: k, BytesUp: 600}); len(res.Exhausted) != 0 {
		t.Fatalf("exhausted early: %+v", res)
	}
	if v := nodeVersion(t, s, n.CertCN); v != v0 {
		t.Fatalf("plain traffic bumped the version: %d -> %d", v0, v)
	}
	if keys, _ := s.ListNodeUserKeys(context.Background(), n.ID); len(keys) != 1 || keys[0] != k {
		t.Fatalf("user below the limit is not on the node: %v", keys)
	}
	res := record(t, s, n.ID, "b-2", model.TrafficDelta{KeyUUID: k, BytesUp: 400})
	if len(res.Exhausted) != 1 || res.Exhausted[0] != (Exhaustion{UserID: u.ID, Used: 1000, Limit: 1000}) {
		t.Fatalf("crossing = %+v", res.Exhausted)
	}
	v1 := nodeVersion(t, s, n.CertCN)
	if v1 <= v0 {
		t.Fatal("crossing the limit did not bump the version")
	}
	if res := record(t, s, n.ID, "b-3", model.TrafficDelta{KeyUUID: k, BytesUp: 50}); len(res.Exhausted) != 0 || nodeVersion(t, s, n.CertCN) != v1 {
		t.Fatal("traffic past the limit bumped again")
	}
	if keys, _ := s.ListNodeUserKeys(context.Background(), n.ID); len(keys) != 0 {
		t.Fatalf("exhausted user still on the node: %v", keys)
	}
}

func TestCounterSaturatesInsteadOfFailing(t *testing.T) {
	s := setupStore(t)
	u, k := userWithKey(t, s)
	pay(t, s, u.ID, mkPlan(t, s).ID, time.Now().UTC())
	n := deltaNode(t, s)
	huge := int64(1) << 62
	record(t, s, n.ID, "b-1", model.TrafficDelta{KeyUUID: k, BytesUp: huge, BytesDown: huge}, model.TrafficDelta{KeyUUID: k, BytesUp: huge})
	record(t, s, n.ID, "b-2", model.TrafficDelta{KeyUUID: k, BytesUp: huge})
	if got := userPeriods(t, s, u.ID)[0].TrafficUsedBytes; got != math.MaxInt64 {
		t.Fatalf("used = %d, want MaxInt64", got)
	}
}

func TestExhaustedUserWaitsForTheNextPeriod(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, k := userWithKey(t, s)
	p := mkLimitedPlan(t, s, 30, 1000)
	now := time.Now().UTC()
	first := pay(t, s, u.ID, p.ID, now.Add(-29*day))
	pay(t, s, u.ID, p.ID, now)
	n := deltaNode(t, s)
	if keys, _ := s.ListNodeUserKeys(ctx, n.ID); len(keys) != 1 {
		t.Fatalf("paid user is not on the node: %v", keys)
	}
	record(t, s, n.ID, "b-1", model.TrafficDelta{KeyUUID: k, BytesUp: 1000})
	if keys, _ := s.ListNodeUserKeys(ctx, n.ID); len(keys) != 0 {
		t.Fatal("exhausted user still on the node before the period ended")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET expires_at = $2 WHERE id = $1`, first.PeriodID, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET starts_at = $1, expires_at = $2 WHERE user_id = $3 AND id <> $4`, now.Add(-time.Second), now.AddDate(0, 0, 30), u.ID, first.PeriodID); err != nil {
		t.Fatal(err)
	}
	v := nodeVersion(t, s, n.CertCN)
	if closed, err := s.ExpireOverdue(ctx, now); err != nil || closed != 1 {
		t.Fatalf("closed = %d, %v", closed, err)
	}
	if nodeVersion(t, s, n.CertCN) <= v {
		t.Fatal("closing the period did not bump the version")
	}
	if keys, _ := s.ListNodeUserKeys(ctx, n.ID); len(keys) != 1 || keys[0] != k {
		t.Fatalf("user did not come back with the next period: %v", keys)
	}
}

func TestCleanupHistoryKeepsFreshData(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	neighbour, neighbourKey := userWithKey(t, s)
	u, oldKey := userWithKey(t, s)
	pay(t, s, u.ID, mkPlan(t, s).ID, time.Now().UTC())
	n := deltaNode(t, s)
	record(t, s, n.ID, "b-old", model.TrafficDelta{KeyUUID: oldKey, BytesUp: 100})
	if _, err := s.RotateUserCredentials(ctx, u.ID, model.NewToken()); err != nil {
		t.Fatal(err)
	}
	record(t, s, n.ID, "b-new", model.TrafficDelta{KeyUUID: oldKey, BytesUp: 1})
	old := time.Now().Add(-40 * day)
	for _, q := range []string{
		`UPDATE traffic_deltas SET reported_at = $1 WHERE id = (SELECT min(id) FROM traffic_deltas)`,
		`UPDATE traffic_batches SET received_at = $1 WHERE batch_id = 'b-old'`,
		`UPDATE user_keys SET revoked_at = $1 WHERE revoked_at IS NOT NULL`,
	} {
		if _, err := s.pool.Exec(ctx, q, old); err != nil {
			t.Fatal(err)
		}
	}
	before := userPeriods(t, s, u.ID)[0].TrafficUsedBytes
	res, err := s.CleanupHistory(ctx, time.Now().Add(-30*day), 1)
	if err != nil {
		t.Fatal(err)
	}
	if res != (CleanupResult{Deltas: 1, Batches: 1, Keys: 1}) {
		t.Fatalf("cleanup = %+v", res)
	}
	var deltas, batches, keys int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM traffic_deltas), (SELECT count(*) FROM traffic_batches), (SELECT count(*) FROM user_keys)`).Scan(&deltas, &batches, &keys); err != nil {
		t.Fatal(err)
	}
	if deltas != 1 || batches != 1 || keys != 2 {
		t.Fatalf("left deltas %d batches %d keys %d", deltas, batches, keys)
	}
	if ks, err := s.GetUserKeys(ctx, neighbour.ID); err != nil || len(ks) != 1 || ks[0].UUID != neighbourKey {
		t.Fatalf("cleanup touched a live key of another user: %+v, %v", ks, err)
	}
	if after := userPeriods(t, s, u.ID)[0].TrafficUsedBytes; after != before {
		t.Fatalf("cleanup changed the counter: %d -> %d", before, after)
	}
	if again, err := s.CleanupHistory(ctx, time.Now().Add(-30*day), 1000); err != nil || again != (CleanupResult{}) {
		t.Fatalf("second cleanup = %+v, %v", again, err)
	}
}

func TestCounterUpdatesDoNotBumpButPeriodChangesDo(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	res := pay(t, s, u.ID, mkPlan(t, s).ID, time.Now().UTC())
	n := deltaNode(t, s)
	v := nodeVersion(t, s, n.CertCN)
	setUsed(t, s, res.PeriodID, 12345)
	if nodeVersion(t, s, n.CertCN) != v {
		t.Fatal("counter update bumped node versions")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET expires_at = expires_at + interval '1 day' WHERE id = $1`, res.PeriodID); err != nil {
		t.Fatal(err)
	}
	if nodeVersion(t, s, n.CertCN) <= v {
		t.Fatal("period change did not bump node versions")
	}
}

func TestIdleExpireTickDoesNotBump(t *testing.T) {
	s := setupStore(t)
	u := mkUser(t, s)
	pay(t, s, u.ID, mkPlan(t, s).ID, time.Now().UTC())
	n := deltaNode(t, s)
	v := nodeVersion(t, s, n.CertCN)
	if closed, err := s.ExpireOverdue(context.Background(), time.Now().UTC()); err != nil || closed != 0 {
		t.Fatalf("closed = %d, %v", closed, err)
	}
	if nodeVersion(t, s, n.CertCN) != v {
		t.Fatal("a tick with nothing to close bumped every node")
	}
}

func TestCleanupWithoutRevokedKeysDoesNotBump(t *testing.T) {
	s := setupStore(t)
	userWithKey(t, s)
	n := deltaNode(t, s)
	v := nodeVersion(t, s, n.CertCN)
	if _, err := s.CleanupHistory(context.Background(), time.Now().Add(-30*day), 100); err != nil {
		t.Fatal(err)
	}
	if nodeVersion(t, s, n.CertCN) != v {
		t.Fatal("cleanup with nothing to delete bumped every node")
	}
}

func TestCleanupStaysFastWhenStatisticsLag(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	deltaNode(t, s)
	addOld := func() {
		t.Helper()
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO traffic_deltas (node_id, key_uuid, bytes_up, bytes_down, reported_at)
			 SELECT (SELECT min(id) FROM nodes), 'k', 1, 1, now() - interval '40 days' FROM generate_series(1, 12000)`); err != nil {
			t.Fatal(err)
		}
	}
	addOld()
	if _, err := s.pool.Exec(ctx, `INSERT INTO traffic_deltas (node_id, key_uuid, bytes_up, bytes_down) SELECT min(id), 'fresh', 1, 1 FROM nodes`); err != nil {
		t.Fatal(err)
	}
	if res, err := s.CleanupHistory(ctx, time.Now().Add(-30*day), 10000); err != nil || res.Deltas != 12000 {
		t.Fatalf("first cleanup = %+v, %v", res, err)
	}
	if _, err := s.pool.Exec(ctx, `VACUUM ANALYZE traffic_deltas`); err != nil {
		t.Fatal(err)
	}
	addOld()
	quick, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, err := s.CleanupHistory(quick, time.Now().Add(-30*day), 10000)
	if err != nil || res.Deltas != 12000 {
		t.Fatalf("cleanup after the table shrank and grew again = %+v, %v; a stale-statistics plan must not hang it", res, err)
	}
}

func waitForLockWaiter(t *testing.T, s *Store, done <-chan struct{}) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		select {
		case <-done:
			return
		default:
		}
		var waiting int
		if err := s.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
	}
	t.Fatal("nobody started waiting for the lock")
}

func TestRotationAndPaymentDoNotDeadlock(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, _ := userWithKey(t, s)
	p := mkPlan(t, s)
	deltaNode(t, s)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE user_keys SET revoked_at = now() WHERE user_id = $1`, u.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var payErr error
	go func() {
		defer close(done)
		_, payErr = s.ApplyPayment(ctx, u.ID, p.ID, time.Now().UTC())
	}()
	waitForLockWaiter(t, s, done)
	if _, err := tx.Exec(ctx, `INSERT INTO user_keys (user_id, uuid) VALUES ($1, $2)`, u.ID, model.NewUUID()); err != nil {
		t.Fatalf("rotation lost to the payment: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	if payErr != nil {
		t.Fatalf("payment lost to the rotation: %v", payErr)
	}
}

func TestConcurrentPaymentsQueueInsteadOfOverlapping(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, p := mkUser(t, s), mkPlan(t, s)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) VALUES ($1, $2, now(), now() + interval '30 days')`, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var res PaymentResult
	var payErr error
	go func() {
		defer close(done)
		res, payErr = s.ApplyPayment(ctx, u.ID, p.ID, time.Now().UTC())
	}()
	waitForLockWaiter(t, s, done)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	if payErr != nil || res.StartsNow {
		t.Fatalf("second payment = %+v, %v: it must wait and queue, not overlap", res, payErr)
	}
	ps := userPeriods(t, s, u.ID)
	if len(ps) != 2 || !sameTime(ps[1].StartsAt, ps[0].ExpiresAt) {
		t.Fatalf("periods overlap: %+v", ps)
	}
}

func TestBatchDuringPeriodSwitchCountsIntoTheNewPeriod(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, k := userWithKey(t, s)
	p := mkPlan(t, s)
	first := pay(t, s, u.ID, p.ID, time.Now().UTC())
	n := deltaNode(t, s)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE subscriptions SET canceled = TRUE, expires_at = now() WHERE id = $1`, first.PeriodID); err != nil {
		t.Fatal(err)
	}
	var fresh int64
	if err := tx.QueryRow(ctx, `INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) VALUES ($1, $2, now(), now() + interval '30 days') RETURNING id`, u.ID, p.ID).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var batchErr error
	go func() {
		defer close(done)
		_, batchErr = s.RecordDeltaBatch(ctx, n.ID, "b-switch", []model.TrafficDelta{{KeyUUID: k, BytesUp: 500}})
	}()
	waitForLockWaiter(t, s, done)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	if batchErr != nil {
		t.Fatal(batchErr)
	}
	for _, period := range userPeriods(t, s, u.ID) {
		if period.ID == fresh && period.TrafficUsedBytes == 500 {
			return
		}
	}
	t.Fatalf("traffic reported during the switch was lost: %+v", userPeriods(t, s, u.ID))
}
