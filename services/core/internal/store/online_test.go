package store

import (
	"context"
	"testing"
	"time"
)

func keyedUser(t *testing.T, s *Store, tg int64, key string) int64 {
	t.Helper()
	u := tgUser(t, s, tg)
	if err := s.AddUserKey(context.Background(), u.ID, key); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestReportOnlineSnapshotHistoryAndPresence(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n1, n2 := mkNode(t, s, "on-1"), mkNode(t, s, "on-2")
	a := keyedUser(t, s, 9501, "11111111-1111-4111-8111-111111111111")
	b := keyedUser(t, s, 9502, "22222222-2222-4222-8222-222222222222")
	start := time.Now().Add(-10 * time.Minute)
	err := s.ReportOnline(ctx, n1.ID, []OnlineEntry{
		{Key: "11111111-1111-4111-8111-111111111111", IP: "203.0.113.5", Since: start},
		{Key: "11111111-1111-4111-8111-111111111111", IP: "2001:db8::5", Since: start.Add(time.Minute)},
		{Key: "22222222-2222-4222-8222-222222222222", IP: "198.51.100.7", Since: start},
		{Key: "ffffffff-ffff-4fff-8fff-ffffffffffff", IP: "192.0.2.1", Since: start},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReportOnline(ctx, n2.ID, []OnlineEntry{{Key: "11111111-1111-4111-8111-111111111111", IP: "203.0.113.5", Since: start}}); err != nil {
		t.Fatal(err)
	}
	p, err := s.UserPresence(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Online || len(p.Now) != 2 || p.LastSeen == nil || p.LastNode == nil {
		t.Fatalf("presence = %+v", p)
	}
	ips := 0
	for _, n := range p.Now {
		ips += len(n.IPs)
	}
	if ips != 3 {
		t.Fatalf("online ips = %d: %+v", ips, p.Now)
	}
	total, byNode, err := s.OnlineCounts(ctx)
	if err != nil || total != 2 || byNode[n1.ID] != 2 || byNode[n2.ID] != 1 {
		t.Fatalf("counts = %d %v, %v", total, byNode, err)
	}

	if err := s.ReportOnline(ctx, n1.ID, []OnlineEntry{{Key: "22222222-2222-4222-8222-222222222222", IP: "198.51.100.7", Since: start}}); err != nil {
		t.Fatal(err)
	}
	if total, byNode, _ = s.OnlineCounts(ctx); total != 2 || byNode[n1.ID] != 1 {
		t.Fatalf("snapshot not replaced: %d %v", total, byNode)
	}
	hist, n, err := s.UserIPHistory(ctx, a, 10, 0)
	if err != nil || n != 3 || len(hist) != 3 {
		t.Fatalf("history = %d %+v, %v", n, hist, err)
	}
	for _, h := range hist {
		if h.FirstSeen.After(h.LastSeen) || h.NodeName == "" {
			t.Fatalf("history row %+v", h)
		}
	}
	if page, n, _ := s.UserIPHistory(ctx, a, 2, 2); n != 3 || len(page) != 1 {
		t.Fatalf("second page = %d %+v", n, page)
	}

	if err := s.ReportOnline(ctx, n2.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportOnline(ctx, n1.ID, []OnlineEntry{}); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.UserPresence(ctx, a); p.Online || p.LastSeen == nil {
		t.Fatalf("after leaving = %+v", p)
	}
	online := true
	list, err := s.ListUsers(ctx, UserFilter{Limit: 10, Online: online})
	if err != nil || len(list) != 0 {
		t.Fatalf("online filter = %+v, %v", list, err)
	}

	if err := s.ReportOnline(ctx, n1.ID, []OnlineEntry{{Key: "22222222-2222-4222-8222-222222222222", IP: "198.51.100.7", Since: start}}); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListUsers(ctx, UserFilter{Limit: 10, Online: online})
	if err != nil || len(list) != 1 || list[0].ID != b || len(list[0].OnlineNodes) != 1 || list[0].OnlineNodes[0].Name != n1.Name {
		t.Fatalf("online list = %+v, %v", list, err)
	}
	users, err := s.NodeOnline(ctx, n1.ID)
	if err != nil || len(users) != 1 || users[0].UserID != b || len(users[0].IPs) != 1 {
		t.Fatalf("node online = %+v, %v", users, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE online_now SET seen_at = now() - interval '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	if total, _, _ = s.OnlineCounts(ctx); total != 0 {
		t.Fatal("a stale snapshot still counts as online")
	}
	if all, _ := s.ListUsers(ctx, UserFilter{Limit: 10}); len(all) < 2 || len(all[0].OnlineNodes) != 0 {
		t.Fatalf("stale online nodes in list: %+v", all)
	}
}

func TestReportOnlineDoesNotBumpNodes(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := mkNode(t, s, "on-3")
	keyedUser(t, s, 9503, "33333333-3333-4333-8333-333333333333")
	before := nodeVersionOf(t, s.pool, "on-3")
	if err := s.ReportOnline(ctx, n.ID, []OnlineEntry{{Key: "33333333-3333-4333-8333-333333333333", IP: "203.0.113.9", Since: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	after := nodeVersionOf(t, s.pool, "on-3")
	if after != before {
		t.Fatal("online report bumped the node version")
	}
}

func TestCleanupOnline(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := mkNode(t, s, "on-4")
	u := keyedUser(t, s, 9504, "44444444-4444-4444-8444-444444444444")
	if err := s.ReportOnline(ctx, n.ID, []OnlineEntry{{Key: "44444444-4444-4444-8444-444444444444", IP: "203.0.113.10", Since: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE ip_history SET first_seen = now() - interval '40 days', last_seen = now() - interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE online_now SET seen_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	removed, err := s.CleanupOnline(ctx, time.Now().AddDate(0, 0, -30))
	if err != nil || removed != 1 {
		t.Fatalf("removed = %d, %v", removed, err)
	}
	var left int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM online_now`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("stale snapshot left: %d, %v", left, err)
	}
	if p, _ := s.UserPresence(ctx, u); p.LastSeen == nil {
		t.Fatal("last seen must outlive the IP history")
	}
}
