package store

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func statsByNode(t *testing.T, s *Store, userID int64) (map[int64]NodeStats, *int64) {
	t.Helper()
	st, err := s.UserStats(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]NodeStats{}
	for _, n := range st.Nodes {
		out[n.NodeID] = n
	}
	return out, st.PeriodID
}

func TestDailyStatsFollowDeltasPerNode(t *testing.T) {
	s := setupStore(t)
	u, k := userWithKey(t, s)
	p := mkPlan(t, s)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	a, b := deltaNode(t, s), deltaNode(t, s)
	record(t, s, a.ID, "a-1", model.TrafficDelta{KeyUUID: k, BytesUp: 100, BytesDown: 1000})
	record(t, s, a.ID, "a-1", model.TrafficDelta{KeyUUID: k, BytesUp: 100, BytesDown: 1000})
	record(t, s, a.ID, "a-2", model.TrafficDelta{KeyUUID: k, BytesUp: 1, BytesDown: 2})
	record(t, s, b.ID, "b-1", model.TrafficDelta{KeyUUID: k, BytesUp: 50})
	record(t, s, b.ID, "b-2", model.TrafficDelta{KeyUUID: model.NewUUID(), BytesUp: 999})
	got, periodID := statsByNode(t, s, u.ID)
	if periodID == nil {
		t.Fatal("no active period in stats")
	}
	if len(got) != 2 {
		t.Fatalf("nodes = %+v", got)
	}
	if n := got[a.ID]; n.Name != a.Name || n.Today != 1103 || n.Days7 != 1103 || n.Days30 != 1103 || n.Period != 1103 {
		t.Fatalf("node a = %+v", n)
	}
	if n := got[b.ID]; n.Today != 50 || n.Period != 50 {
		t.Fatalf("node b = %+v", n)
	}
	periods := userPeriods(t, s, u.ID)
	if sum := got[a.ID].Period + got[b.ID].Period; sum != periods[0].TrafficUsedBytes {
		t.Fatalf("period by nodes %d != counter %d", sum, periods[0].TrafficUsedBytes)
	}
}

func TestDailyStatsWindows(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, _ := userWithKey(t, s)
	n := deltaNode(t, s)
	for _, row := range []struct {
		ago   int
		bytes int64
	}{{0, 1}, {1, 2}, {6, 10}, {7, 100}, {29, 1000}, {30, 10000}} {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO traffic_daily (user_id, node_id, day, bytes_up) VALUES ($1, $2, (now() AT TIME ZONE 'Europe/Moscow')::date - $3::int, $4)`,
			u.ID, n.ID, row.ago, row.bytes); err != nil {
			t.Fatal(err)
		}
	}
	got, periodID := statsByNode(t, s, u.ID)
	if periodID != nil {
		t.Fatalf("period without a subscription: %v", *periodID)
	}
	if st := got[n.ID]; st.Today != 1 || st.Days7 != 13 || st.Days30 != 1113 || st.Period != 0 {
		t.Fatalf("windows = %+v", st)
	}
}

func TestDailyStatsSplitByPeriod(t *testing.T) {
	s := setupStore(t)
	u, k := userWithKey(t, s)
	first, second := mkPlan(t, s), mkPlan(t, s)
	pay(t, s, u.ID, first.ID, time.Now().UTC())
	n := deltaNode(t, s)
	record(t, s, n.ID, "p-1", model.TrafficDelta{KeyUUID: k, BytesUp: 700})
	pay(t, s, u.ID, second.ID, time.Now().UTC())
	record(t, s, n.ID, "p-2", model.TrafficDelta{KeyUUID: k, BytesUp: 40})
	got, _ := statsByNode(t, s, u.ID)
	if st := got[n.ID]; st.Today != 740 || st.Period != 40 {
		t.Fatalf("after the period switch = %+v", st)
	}
	var rows int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM traffic_daily WHERE user_id = $1`, u.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("daily rows = %d, want one per period", rows)
	}
}

func TestDailyStatsWithoutActivePeriod(t *testing.T) {
	s := setupStore(t)
	u, k := userWithKey(t, s)
	n := deltaNode(t, s)
	record(t, s, n.ID, "x-1", model.TrafficDelta{KeyUUID: k, BytesDown: 5})
	got, periodID := statsByNode(t, s, u.ID)
	if periodID != nil || got[n.ID].Today != 5 || got[n.ID].Period != 0 {
		t.Fatalf("stats = %+v, period %v", got, periodID)
	}
}

func TestDailyStatsDayFollowsTheTimezone(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, k := userWithKey(t, s)
	n := deltaNode(t, s)
	days := map[string]string{}
	for i, tz := range []string{"Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		if err := s.SetStatsTimezone(ctx, tz); err != nil {
			t.Fatal(err)
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Fatal(err)
		}
		before := time.Now().In(loc).Format(time.DateOnly)
		record(t, s, n.ID, "tz-"+tz, model.TrafficDelta{KeyUUID: k, BytesUp: int64(i + 1)})
		after := time.Now().In(loc).Format(time.DateOnly)
		var day time.Time
		if err := s.pool.QueryRow(ctx, `SELECT day FROM traffic_daily WHERE user_id = $1 AND bytes_up = $2`, u.ID, i+1).Scan(&day); err != nil {
			t.Fatal(err)
		}
		got := day.Format(time.DateOnly)
		if got != before && got != after {
			t.Fatalf("day = %s, want %s in %s", got, before, tz)
		}
		days[tz] = got
	}
	if days["Pacific/Kiritimati"] == days["Pacific/Pago_Pago"] {
		t.Fatalf("UTC+14 and UTC-11 gave the same day %s", days["Pacific/Kiritimati"])
	}
	if err := s.SetStatsTimezone(ctx, "Mars/Olympus"); err == nil {
		t.Fatal("unknown timezone accepted")
	}
}

func TestCleanupDailyStats(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, _ := userWithKey(t, s)
	n := deltaNode(t, s)
	for _, ago := range []int{0, 364, 365, 400} {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO traffic_daily (user_id, node_id, day, bytes_up) VALUES ($1, $2, (now() AT TIME ZONE 'Europe/Moscow')::date - $3::int, 1)`,
			u.ID, n.ID, ago); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := s.CleanupDailyStats(ctx, 365)
	if err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM traffic_daily`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if deleted != 2 || left != 2 {
		t.Fatalf("deleted %d, left %d", deleted, left)
	}
}
