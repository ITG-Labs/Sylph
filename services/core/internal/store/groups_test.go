package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkGroup(t *testing.T, s *Store, name string) *model.LocationGroup {
	t.Helper()
	g, err := s.CreateLocationGroup(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func groupNames(t *testing.T, s *Store) []string {
	t.Helper()
	list, err := s.ListLocationGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, g := range list {
		out = append(out, g.Name)
	}
	return out
}

func TestRenameGroup(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g1, g2 := mkGroup(t, s, "eu"), mkGroup(t, s, "asia")
	n := deltaNode(t, s)
	if err := s.SetNodeGroups(ctx, n.ID, []int64{g1.ID}); err != nil {
		t.Fatal(err)
	}
	before := nodeVersion(t, s, n.CertCN)
	got, err := s.RenameLocationGroup(ctx, g1.ID, "Европа")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != g1.ID || got.Name != "Европа" || !got.CreatedAt.Equal(g1.CreatedAt) {
		t.Fatalf("renamed = %+v", got)
	}
	if v := nodeVersion(t, s, n.CertCN); v != before {
		t.Fatalf("rename bumped the node %d -> %d", before, v)
	}
	if _, err := s.RenameLocationGroup(ctx, g1.ID, g2.Name); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := s.RenameLocationGroup(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing group: %v", err)
	}
}

func TestDeleteOnlyEmptyGroup(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	withNode, withPlan, empty := mkGroup(t, s, "nodes"), mkGroup(t, s, "plans"), mkGroup(t, s, "empty")
	n := deltaNode(t, s)
	if err := s.SetNodeGroups(ctx, n.ID, []int64{withNode.ID}); err != nil {
		t.Fatal(err)
	}
	p := mkPlan(t, s)
	p.LocationGroupID = &withPlan.ID
	if err := s.UpdatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{withNode.ID, withPlan.ID} {
		if err := s.DeleteLocationGroup(ctx, id); !errors.Is(err, ErrGroupNotEmpty) {
			t.Fatalf("delete non-empty %d: %v", id, err)
		}
	}
	before := nodeVersion(t, s, n.CertCN)
	if err := s.DeleteLocationGroup(ctx, empty.ID); err != nil {
		t.Fatal(err)
	}
	if v := nodeVersion(t, s, n.CertCN); v != before {
		t.Fatalf("delete bumped the node %d -> %d", before, v)
	}
	if got := groupNames(t, s); len(got) != 3 || !slices.Contains(got, "nodes") || !slices.Contains(got, "plans") || slices.Contains(got, "empty") {
		t.Fatalf("groups = %v", got)
	}
	if err := s.DeleteLocationGroup(ctx, empty.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if err := s.SetNodeGroups(ctx, n.ID, []int64{empty.ID}); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("move into a deleted group: %v", err)
	}
}

func TestDeleteGroupWaitsForAMoveInto(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	warmPool(t, s)
	g := mkGroup(t, s, "g")
	n := deltaNode(t, s)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO group_nodes (group_id, node_id) VALUES ($1, $2)`, g.ID, n.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var delErr error
	go func() {
		defer close(done)
		delErr = s.DeleteLocationGroup(ctx, g.ID)
	}()
	waitForLockWaiter(t, s, done)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("delete did not finish")
	}
	if !errors.Is(delErr, ErrGroupNotEmpty) {
		t.Fatalf("delete during a move: %v", delErr)
	}
	if got := groupNames(t, s); len(got) != 1 {
		t.Fatalf("groups = %v", got)
	}
}

func TestPlanGroupMoveBumpsAllNodes(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g")
	a, b := deltaNode(t, s), deltaNode(t, s)
	p := mkPlan(t, s)
	va, vb := nodeVersion(t, s, a.CertCN), nodeVersion(t, s, b.CertCN)
	p.LocationGroupID = &g.ID
	if err := s.UpdatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if nodeVersion(t, s, a.CertCN) == va || nodeVersion(t, s, b.CertCN) == vb {
		t.Fatal("moving a plan did not bump every node")
	}
}

func TestAdminPlansCountActiveUsers(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	p1, p2 := mkPlan(t, s), mkPlan(t, s)
	now := time.Now().UTC()
	a, b, c := mkUser(t, s), mkUser(t, s), mkUser(t, s)
	pay(t, s, a.ID, p1.ID, now)
	pay(t, s, a.ID, p1.ID, now)
	pay(t, s, b.ID, p1.ID, now)
	pay(t, s, c.ID, p1.ID, now.Add(-40*24*time.Hour))
	d := mkUser(t, s)
	pay(t, s, d.ID, p1.ID, now)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET status = 'banned' WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAdminPlans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int64]int{}
	for _, p := range list {
		counts[p.ID] = p.ActiveUsers
	}
	if len(list) != 2 || counts[p1.ID] != 2 || counts[p2.ID] != 0 {
		t.Fatalf("active users = %v", counts)
	}
	if list[0].Name != p1.Name {
		t.Fatalf("plan fields are not filled: %+v", list[0])
	}
}
