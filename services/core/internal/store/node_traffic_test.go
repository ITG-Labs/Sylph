package store

import (
	"context"
	"math"
	"testing"
	"time"
)

func insertDaily(t *testing.T, s *Store, userID, nodeID, periodID int64, ago int, up, down int64) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`INSERT INTO traffic_daily (user_id, node_id, period_id, day, bytes_up, bytes_down)
		 VALUES ($1, $2, $3, (now() AT TIME ZONE 'Europe/Moscow')::date - $4::int, $5, $6)`,
		userID, nodeID, periodID, ago, up, down); err != nil {
		t.Fatal(err)
	}
}

func topIDs(top []TopUser) []int64 {
	out := []int64{}
	for _, u := range top {
		out = append(out, u.UserID)
	}
	return out
}

func sameIDs(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestNodeTrafficWindowsDailyAndTop(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n, other := deltaNode(t, s), deltaNode(t, s)
	a, b := tgUser(t, s, 4001), tgUser(t, s, 4002)
	if err := s.SaveTelegramProfile(ctx, b.ID, TelegramProfile{Username: strp("bob"), FirstName: "Боб"}); err != nil {
		t.Fatal(err)
	}
	insertDaily(t, s, a.ID, n.ID, 0, 0, 1, 2)
	insertDaily(t, s, a.ID, n.ID, 7, 0, 3, 0)
	insertDaily(t, s, a.ID, n.ID, 0, 6, 10, 0)
	insertDaily(t, s, a.ID, n.ID, 0, 7, 100, 0)
	insertDaily(t, s, a.ID, n.ID, 0, 29, 1000, 0)
	insertDaily(t, s, a.ID, n.ID, 0, 30, 10000, 0)
	insertDaily(t, s, b.ID, n.ID, 0, 0, 0, 5)
	insertDaily(t, s, b.ID, n.ID, 0, 3, 50, 0)
	insertDaily(t, s, b.ID, other.ID, 0, 0, 7777, 7777)

	st, err := s.NodeTraffic(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Today != (UpDown{Up: 4, Down: 7}) || st.Days7 != (UpDown{Up: 64, Down: 7}) || st.Days30 != (UpDown{Up: 1164, Down: 7}) {
		t.Fatalf("windows = %+v %+v %+v", st.Today, st.Days7, st.Days30)
	}
	if len(st.Daily) != 30 {
		t.Fatalf("daily has %d days", len(st.Daily))
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	today := time.Now().In(loc).Format(time.DateOnly)
	if st.Daily[29].Day != today || st.Daily[29].UpDown != (UpDown{Up: 4, Down: 7}) {
		t.Fatalf("last day = %+v, today %s", st.Daily[29], today)
	}
	for i, want := range map[int]UpDown{0: {Up: 1000}, 1: {}, 22: {Up: 100}, 23: {Up: 10}, 26: {Up: 50}, 28: {}} {
		if st.Daily[i].UpDown != want {
			t.Fatalf("daily[%d] = %+v, want %+v", i, st.Daily[i], want)
		}
	}
	if st.Daily[0].Day >= st.Daily[1].Day {
		t.Fatalf("daily is not ascending: %s, %s", st.Daily[0].Day, st.Daily[1].Day)
	}
	if got := topIDs(st.Top.Today); !sameIDs(got, []int64{a.ID, b.ID}) || st.Top.Today[0].Bytes != 6 || st.Top.Today[1].Bytes != 5 {
		t.Fatalf("top today = %+v", st.Top.Today)
	}
	if got := topIDs(st.Top.Days7); !sameIDs(got, []int64{b.ID, a.ID}) || st.Top.Days7[0].Bytes != 55 {
		t.Fatalf("top 7 = %+v", st.Top.Days7)
	}
	if got := topIDs(st.Top.Days30); !sameIDs(got, []int64{a.ID, b.ID}) || st.Top.Days30[0].Bytes != 1116 {
		t.Fatalf("top 30 = %+v", st.Top.Days30)
	}
	bob := st.Top.Today[1]
	if bob.TelegramID == nil || *bob.TelegramID != 4002 || bob.TelegramUsername == nil || *bob.TelegramUsername != "bob" || bob.TelegramName == nil || *bob.TelegramName != "Боб" {
		t.Fatalf("top user = %+v", bob)
	}
	if st.Top.Today[0].TelegramUsername != nil || st.Top.Today[0].TelegramName != nil {
		t.Fatalf("user without a profile = %+v", st.Top.Today[0])
	}
}

func TestNodeTrafficTopIsTenHeaviest(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := deltaNode(t, s)
	var ids []int64
	for i := range 12 {
		u := tgUser(t, s, int64(5000+i))
		ids = append(ids, u.ID)
		insertDaily(t, s, u.ID, n.ID, 0, 0, int64(100+i%3), 0)
	}
	st, err := s.NodeTraffic(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{ids[2], ids[5], ids[8], ids[11], ids[1], ids[4], ids[7], ids[10], ids[0], ids[3]}
	if got := topIDs(st.Top.Today); !sameIDs(got, want) {
		t.Fatalf("top = %v, want %v", got, want)
	}
}

func TestNodeTrafficEmptyAndSaturated(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := deltaNode(t, s)
	st, err := s.NodeTraffic(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Today != (UpDown{}) || len(st.Daily) != 30 || st.Top.Today == nil || len(st.Top.Days30) != 0 {
		t.Fatalf("empty node = %+v", st)
	}
	u, v := tgUser(t, s, 6001), tgUser(t, s, 6002)
	insertDaily(t, s, u.ID, n.ID, 0, 0, math.MaxInt64, math.MaxInt64)
	insertDaily(t, s, v.ID, n.ID, 0, 1, math.MaxInt64, 0)
	st, err = s.NodeTraffic(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Today.Up != math.MaxInt64 || st.Days7.Up != math.MaxInt64 || st.Top.Today[0].Bytes != math.MaxInt64 {
		t.Fatalf("saturation = %+v %+v %+v", st.Today, st.Days7, st.Top.Today)
	}
}

func TestNodeTrafficDaysFollowTheStatsZone(t *testing.T) {
	for _, zone := range []string{"Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		s := setupStore(t)
		ctx := context.Background()
		if err := s.SetStatsTimezone(ctx, zone); err != nil {
			t.Fatal(err)
		}
		n := deltaNode(t, s)
		u := tgUser(t, s, 9001)
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO traffic_daily (user_id, node_id, day, bytes_up) VALUES ($1, $2, (now() AT TIME ZONE $3)::date, 5)`,
			u.ID, n.ID, zone); err != nil {
			t.Fatal(err)
		}
		st, err := s.NodeTraffic(ctx, n.ID)
		if err != nil {
			t.Fatal(err)
		}
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		if today := time.Now().In(loc).Format(time.DateOnly); st.Daily[29].Day != today || st.Today.Up != 5 || len(st.Top.Today) != 1 {
			t.Fatalf("%s: last day %s (want %s), today %+v, top %+v", zone, st.Daily[29].Day, today, st.Today, st.Top.Today)
		}
	}
}
