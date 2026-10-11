package store

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func nodeVersion(t *testing.T, s *Store, cn string) int64 {
	t.Helper()
	n, err := s.GetNodeByCN(context.Background(), cn)
	if err != nil {
		t.Fatal(err)
	}
	return n.UsersVersion
}

func twoNodes(t *testing.T, s *Store) (*model.Node, *model.Node) {
	t.Helper()
	ctx := context.Background()
	n1 := &model.Node{CertCN: "node-1", Name: "n1"}
	n2 := &model.Node{CertCN: "node-2", Name: "n2"}
	if err := s.UpsertNode(ctx, n1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(ctx, n2); err != nil {
		t.Fatal(err)
	}
	return n1, n2
}

func TestGroupChangeBumpsOnlyThatNode(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n1, _ := twoNodes(t, s)
	g, err := s.CreateLocationGroup(ctx, "premium")
	if err != nil {
		t.Fatal(err)
	}
	before1, before2 := nodeVersion(t, s, "node-1"), nodeVersion(t, s, "node-2")
	n1.GroupID = &g.ID
	if err := s.UpsertNode(ctx, n1); err != nil {
		t.Fatal(err)
	}
	if got := nodeVersion(t, s, "node-1"); got <= before1 {
		t.Fatalf("node-1 version %d not bumped from %d", got, before1)
	}
	if n1.UsersVersion != nodeVersion(t, s, "node-1") {
		t.Fatalf("upsert returned version %d, stored %d", n1.UsersVersion, nodeVersion(t, s, "node-1"))
	}
	if got := nodeVersion(t, s, "node-2"); got != before2 {
		t.Fatalf("node-2 version changed %d -> %d", before2, got)
	}
}

func TestAddressAndEnabledDoNotBump(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n1, _ := twoNodes(t, s)
	before := nodeVersion(t, s, "node-1")
	n1.Address, n1.Enabled = "other.example.com", false
	if err := s.UpdateNode(ctx, n1); err != nil {
		t.Fatal(err)
	}
	if got := nodeVersion(t, s, "node-1"); got != before {
		t.Fatalf("address/enabled change bumped: %d -> %d", before, got)
	}
}

func TestUpsertWithoutChangesDoesNotBump(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	twoNodes(t, s)
	before := nodeVersion(t, s, "node-1")
	again := &model.Node{CertCN: "node-1", Name: "renamed"}
	if err := s.UpsertNode(ctx, again); err != nil {
		t.Fatal(err)
	}
	if got := nodeVersion(t, s, "node-1"); got != before {
		t.Fatalf("rename-only upsert bumped: %d -> %d", before, got)
	}
}

func TestStatusColumnsDoNotBump(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n1, _ := twoNodes(t, s)
	before := nodeVersion(t, s, "node-1")
	if _, err := s.pool.Exec(ctx,
		`UPDATE nodes SET last_heartbeat = now(), agent_version = 'v1', xray_version = 'x', active_users = 3,
		 agent_uptime_sec = 10, xray_uptime_sec = 20, last_sync_at = now() WHERE id = $1`, n1.ID); err != nil {
		t.Fatal(err)
	}
	if got := nodeVersion(t, s, "node-1"); got != before {
		t.Fatalf("status update bumped: %d -> %d", before, got)
	}
}

func TestVersionFollowsClockAndSurvivesRestore(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	twoNodes(t, s)
	start := time.Now().Add(-time.Minute).UnixMicro()
	mkUser(t, s)
	v1 := nodeVersion(t, s, "node-1")
	if v1 < start {
		t.Fatalf("version %d is not clock based (want >= %d)", v1, start)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE nodes SET users_version = $1`, v1-1_000_000); err != nil {
		t.Fatal(err)
	}
	mkUser(t, s)
	if v2 := nodeVersion(t, s, "node-1"); v2 <= v1 {
		t.Fatalf("version after restore %d not above pre-restore %d", v2, v1)
	}
}

func TestVersionStrictlyGrowsWithinTransaction(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	twoNodes(t, s)
	mkUser(t, s)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var prev int64
	for i := 0; i < 3; i++ {
		if _, err := tx.Exec(ctx, `UPDATE users SET status = status`); err != nil {
			t.Fatal(err)
		}
		var v int64
		if err := tx.QueryRow(ctx, `SELECT users_version FROM nodes WHERE cert_cn = 'node-1'`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v <= prev {
			t.Fatalf("bump %d: version %d not above %d", i, v, prev)
		}
		prev = v
	}
}

func TestVersionGrowsWhenClockBehind(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	twoNodes(t, s)
	future := time.Now().Add(24 * time.Hour).UnixMicro()
	if _, err := s.pool.Exec(ctx, `UPDATE nodes SET users_version = $1`, future); err != nil {
		t.Fatal(err)
	}
	mkUser(t, s)
	if got := nodeVersion(t, s, "node-1"); got != future+1 {
		t.Fatalf("version = %d, want %d (+1 when clock is behind)", got, future+1)
	}
}
