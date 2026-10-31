package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkInbound(t *testing.T, s *Store, nodeID int64, tag string) *model.NodeInbound {
	t.Helper()
	in := realityInbound(nodeID, tag)
	if err := s.CreateInbound(context.Background(), in, nil); err != nil {
		t.Fatal(err)
	}
	return in
}

func setMembers(t *testing.T, s *Store, groupID int64, nodes []int64, inbounds []int64) {
	t.Helper()
	if err := s.SetGroupMembers(context.Background(), groupID, nodes, inbounds); err != nil {
		t.Fatal(err)
	}
}

func setPoolUsed(t *testing.T, s *Store, periodID, poolID, used int64) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`INSERT INTO period_pool_usage (period_id, pool_id, used_bytes) VALUES ($1, $2, $3)
		 ON CONFLICT (period_id, pool_id) DO UPDATE SET used_bytes = EXCLUDED.used_bytes`, periodID, poolID, used); err != nil {
		t.Fatal(err)
	}
}

func mkBareNode(t *testing.T, s *Store, cn string) *model.Node {
	t.Helper()
	n := mkNode(t, s, cn)
	if _, err := s.pool.Exec(context.Background(), `DELETE FROM group_nodes WHERE node_id = $1`, n.ID); err != nil {
		t.Fatal(err)
	}
	return n
}

func mkGroupPlan(t *testing.T, s *Store, name string, group int64, pools ...model.PlanPool) *model.Plan {
	t.Helper()
	p := &model.Plan{Name: name, PriceRub: 1, PeriodDays: 30, LocationGroupID: &group, Pools: pools}
	if err := s.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func lim(v int64) *int64 { return &v }

func keyInbounds(t *testing.T, s *Store, nodeID int64, key string) []string {
	t.Helper()
	keys, err := s.ListNodeKeys(context.Background(), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if k.UUID == key {
			return k.Inbounds
		}
	}
	return nil
}

func TestGroupMembersAreNodesAndInbounds(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "eu")
	n1, n2 := mkBareNode(t, s, "n1"), mkBareNode(t, s, "n2")
	mkInbound(t, s, n1.ID, "a")
	b := mkInbound(t, s, n2.ID, "b")
	before := nodeVersion(t, s, "n2")
	setMembers(t, s, g.ID, []int64{n1.ID}, []int64{b.ID})
	if nodeVersion(t, s, "n2") == before {
		t.Fatal("membership change must resync the nodes")
	}
	groups, err := s.ListLocationGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || !slices.Equal(groups[1].NodeIDs, []int64{n1.ID}) || !slices.Equal(groups[1].InboundIDs, []int64{b.ID}) {
		t.Fatalf("groups = %+v", groups)
	}
	got, err := s.GetNode(ctx, n1.ID)
	if err != nil || !slices.Equal(got.GroupIDs, []int64{g.ID}) {
		t.Fatalf("node groups = %+v, %v", got, err)
	}
	if got, _ := s.GetNode(ctx, n2.ID); len(got.GroupIDs) != 0 {
		t.Fatalf("an inbound member must not make its node a member: %v", got.GroupIDs)
	}
	setMembers(t, s, g.ID, nil, nil)
	if groups, _ := s.ListLocationGroups(ctx); len(groups[1].NodeIDs) != 0 || len(groups[1].InboundIDs) != 0 {
		t.Fatalf("members not replaced: %+v", groups[1])
	}
	if err := s.SetGroupMembers(ctx, g.ID, []int64{999999}, nil); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("unknown node: %v", err)
	}
	if err := s.SetGroupMembers(ctx, 999999, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown group: %v", err)
	}
}

func TestSetNodeGroupsReplacesWholeNodeMemberships(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g1, g2 := mkGroup(t, s, "g1"), mkGroup(t, s, "g2")
	n := mkBareNode(t, s, "n1")
	in := mkInbound(t, s, n.ID, "a")
	setMembers(t, s, g1.ID, nil, []int64{in.ID})
	if err := s.SetNodeGroups(ctx, n.ID, []int64{g2.ID, g1.ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetNode(ctx, n.ID); !slices.Equal(got.GroupIDs, []int64{g1.ID, g2.ID}) {
		t.Fatalf("groups = %v", got.GroupIDs)
	}
	if err := s.SetNodeGroups(ctx, n.ID, []int64{g2.ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetNode(ctx, n.ID); !slices.Equal(got.GroupIDs, []int64{g2.ID}) {
		t.Fatalf("groups = %v", got.GroupIDs)
	}
	if groups, _ := s.ListLocationGroups(ctx); !slices.Equal(groups[1].InboundIDs, []int64{in.ID}) {
		t.Fatalf("inbound memberships must survive: %+v", groups[1])
	}
	if err := s.SetNodeGroups(ctx, n.ID, []int64{999999}); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("unknown group: %v", err)
	}
}

func TestDeleteGroupRefusedWhileUsed(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	byInbound, byPlan, free := mkGroup(t, s, "inbound"), mkGroup(t, s, "plan"), mkGroup(t, s, "free")
	n := mkBareNode(t, s, "n1")
	in := mkInbound(t, s, n.ID, "a")
	setMembers(t, s, byInbound.ID, nil, []int64{in.ID})
	mkGroupPlan(t, s, "p", byPlan.ID)
	for _, id := range []int64{byInbound.ID, byPlan.ID} {
		if err := s.DeleteLocationGroup(ctx, id); !errors.Is(err, ErrGroupNotEmpty) {
			t.Fatalf("delete used group %d: %v", id, err)
		}
	}
	if err := s.DeleteLocationGroup(ctx, free.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPlanPoolsRoundTrip(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g")
	n := mkBareNode(t, s, "n1")
	a, b, c := mkInbound(t, s, n.ID, "a"), mkInbound(t, s, n.ID, "b"), mkInbound(t, s, n.ID, "c")
	setMembers(t, s, g.ID, []int64{n.ID}, nil)
	p := mkGroupPlan(t, s, "premium", g.ID,
		model.PlanPool{Name: "Премиум", InboundIDs: []int64{b.ID, a.ID}, TrafficLimitBytes: lim(100)},
		model.PlanPool{Name: "Без лимита", InboundIDs: []int64{c.ID}})
	got, err := s.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pools) != 2 || got.Pools[0].Name != "Премиум" || !slices.Equal(got.Pools[0].InboundIDs, []int64{a.ID, b.ID}) ||
		*got.Pools[0].TrafficLimitBytes != 100 || !slices.Equal(got.Pools[1].InboundIDs, []int64{c.ID}) || got.Pools[1].TrafficLimitBytes != nil ||
		got.Pools[0].ID == 0 || got.FreeInbounds != 0 {
		t.Fatalf("pools = %+v, free %d", got.Pools, got.FreeInbounds)
	}
	kept := got.Pools[1]
	kept.Name = "Остальное"
	kept.InboundIDs = []int64{a.ID, c.ID}
	got.Pools = []model.PlanPool{kept, {Name: "Новый", InboundIDs: []int64{b.ID}}}
	if err := s.UpdatePlan(ctx, got); err != nil {
		t.Fatalf("moving an inbound between pools in one save: %v", err)
	}
	again, err := s.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Pools) != 2 || again.Pools[0].ID != kept.ID || again.Pools[0].Name != "Остальное" ||
		!slices.Equal(again.Pools[0].InboundIDs, []int64{a.ID, c.ID}) || !slices.Equal(again.Pools[1].InboundIDs, []int64{b.ID}) {
		t.Fatalf("pools after update = %+v", again.Pools)
	}
	again.Pools = again.Pools[:1]
	if err := s.UpdatePlan(ctx, again); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPlan(ctx, p.ID); got.FreeInbounds != 1 {
		t.Fatalf("an inbound left outside every pool must count as free: %d", got.FreeInbounds)
	}
	plans, err := s.ListPlans(ctx)
	if err != nil || len(plans) != 1 || len(plans[0].Pools) != 1 {
		t.Fatalf("listed = %+v, %v", plans, err)
	}
	foreign := mkPlan(t, s)
	foreign.Pools = []model.PlanPool{{ID: kept.ID, Name: "x"}}
	if err := s.UpdatePlan(ctx, foreign); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("stealing another plan's pool: %v", err)
	}
	foreign.Pools = []model.PlanPool{{Name: "x", InboundIDs: []int64{999999}}}
	if err := s.UpdatePlan(ctx, foreign); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("unknown inbound: %v", err)
	}
}

func TestPlanPoolsMustNotOverlap(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g")
	n := mkBareNode(t, s, "n1")
	a, b := mkInbound(t, s, n.ID, "a"), mkInbound(t, s, n.ID, "b")
	setMembers(t, s, g.ID, []int64{n.ID}, nil)
	p := mkGroupPlan(t, s, "p", g.ID)
	p.Pools = []model.PlanPool{{Name: "x", InboundIDs: []int64{a.ID}}, {Name: "y", InboundIDs: []int64{a.ID, b.ID}}}
	if err := s.UpdatePlan(ctx, p); !errors.Is(err, ErrPoolOverlap) {
		t.Fatalf("overlapping pools: %v", err)
	}
	if got, _ := s.GetPlan(ctx, p.ID); len(got.Pools) != 0 {
		t.Fatalf("rejected pools were stored: %+v", got.Pools)
	}
	other := mkGroupPlan(t, s, "other", g.ID, model.PlanPool{Name: "x", InboundIDs: []int64{a.ID}})
	p.Pools = []model.PlanPool{{Name: "x", InboundIDs: []int64{a.ID}}}
	if err := s.UpdatePlan(ctx, p); err != nil {
		t.Fatalf("two plans may pool the same inbound: %v, other %d", err, other.ID)
	}
}

func TestNodeKeysFollowGroupsAndPools(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g, h := mkGroup(t, s, "g"), mkGroup(t, s, "h")
	n1, n2 := mkBareNode(t, s, "n1"), mkBareNode(t, s, "n2")
	x := mkInbound(t, s, n1.ID, "x")
	a := mkInbound(t, s, n2.ID, "a")
	b := mkInbound(t, s, n2.ID, "b")
	mkInbound(t, s, n2.ID, "c")
	setMembers(t, s, g.ID, []int64{n1.ID}, []int64{a.ID})
	setMembers(t, s, h.ID, nil, []int64{b.ID})
	premium := mkGroupPlan(t, s, "premium", g.ID, model.PlanPool{Name: "n1", InboundIDs: []int64{x.ID}, TrafficLimitBytes: lim(100)})
	cheap := mkGroupPlan(t, s, "cheap", h.ID)
	u1, k1 := userWithKey(t, s)
	u2, k2 := userWithKey(t, s)
	period := pay(t, s, u1.ID, premium.ID, time.Now().UTC()).PeriodID
	pay(t, s, u2.ID, cheap.ID, time.Now().UTC())
	if got := keyInbounds(t, s, n1.ID, k1); !slices.Equal(got, []string{"x"}) {
		t.Fatalf("premium on n1 = %v", got)
	}
	if got := keyInbounds(t, s, n2.ID, k1); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("premium on n2 = %v (c is in no group, b only in h)", got)
	}
	if got := keyInbounds(t, s, n1.ID, k2); got != nil {
		t.Fatalf("cheap must not see n1: %v", got)
	}
	if got := keyInbounds(t, s, n2.ID, k2); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("cheap on n2 = %v", got)
	}
	setPoolUsed(t, s, period, premium.Pools[0].ID, 100)
	if got := keyInbounds(t, s, n1.ID, k1); got != nil {
		t.Fatalf("exhausted pool still open on n1: %v", got)
	}
	if got := keyInbounds(t, s, n2.ID, k1); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("an inbound outside every pool must stay open: %v", got)
	}
	if sub, err := s.GetActiveSubscription(ctx, u1.ID); err != nil || sub.TrafficExhausted {
		t.Fatalf("an open inbound outside pools keeps the period alive: %+v, %v", sub, err)
	}
}

func TestPlanWithoutGroupSeesNothing(t *testing.T) {
	s := setupStore(t)
	n := mkBareNode(t, s, "n1")
	mkInbound(t, s, n.ID, "a")
	p := &model.Plan{Name: "none", PriceRub: 1, PeriodDays: 30}
	if err := s.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	u, k := userWithKey(t, s)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	if got := keyInbounds(t, s, n.ID, k); got != nil {
		t.Fatalf("a plan without a group must see nothing, an inbound without a group must be seen by nobody: %v", got)
	}
	if sub, err := s.GetActiveSubscription(context.Background(), u.ID); err != nil || sub.TrafficExhausted {
		t.Fatalf("a plan that sees nothing is not exhausted: %+v, %v", sub, err)
	}
}

func TestLegacyNodeKeysOnlyWithEveryInbound(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g")
	n := mkBareNode(t, s, "n1")
	a := mkInbound(t, s, n.ID, "a")
	b := mkInbound(t, s, n.ID, "b")
	setMembers(t, s, g.ID, nil, []int64{a.ID})
	p := mkGroupPlan(t, s, "p", g.ID)
	u, k := userWithKey(t, s)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	keys, err := s.ListNodeKeysLegacy(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("a key that sees only part of the node must not reach an old agent: %+v", keys)
	}
	setMembers(t, s, g.ID, nil, []int64{a.ID, b.ID})
	keys, err = s.ListNodeKeysLegacy(ctx, n.ID)
	if err != nil || len(keys) != 1 || keys[0].UUID != k {
		t.Fatalf("keys = %+v, %v", keys, err)
	}
}

func TestDeltasLandInTheInboundsPool(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g")
	n1, n2 := mkBareNode(t, s, "n1"), mkBareNode(t, s, "n2")
	x := mkInbound(t, s, n1.ID, "x")
	mkInbound(t, s, n2.ID, "a")
	setMembers(t, s, g.ID, []int64{n1.ID, n2.ID}, nil)
	p := mkGroupPlan(t, s, "premium", g.ID, model.PlanPool{Name: "n1", InboundIDs: []int64{x.ID}, TrafficLimitBytes: lim(100)})
	pool := p.Pools[0].ID
	u, k := userWithKey(t, s)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	record(t, s, n1.ID, "b1", model.TrafficDelta{KeyUUID: k, Inbound: "x", BytesUp: 30, BytesDown: 30})
	record(t, s, n2.ID, "b2", model.TrafficDelta{KeyUUID: k, Inbound: "a", BytesDown: 10})
	record(t, s, n1.ID, "b3", model.TrafficDelta{KeyUUID: k, BytesDown: 5})
	sub, err := s.GetActiveSubscription(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub.TrafficUsedBytes != 10 || sub.MainIdle || len(sub.Pools) != 1 || sub.Pools[0].PoolID != pool || sub.Pools[0].UsedBytes != 65 {
		t.Fatalf("outside pools = %d, pools = %+v", sub.TrafficUsedBytes, sub.Pools)
	}
	v1, v2 := nodeVersion(t, s, "n1"), nodeVersion(t, s, "n2")
	res := record(t, s, n1.ID, "b4", model.TrafficDelta{KeyUUID: k, Inbound: "x", BytesDown: 40})
	if len(res.Exhausted) != 1 || res.Exhausted[0].PoolID == nil || *res.Exhausted[0].PoolID != pool || res.Exhausted[0].Used != 105 {
		t.Fatalf("exhausted = %+v", res.Exhausted)
	}
	if nodeVersion(t, s, "n1") == v1 {
		t.Fatal("the node behind the exhausted pool was not resynced")
	}
	if nodeVersion(t, s, "n2") != v2 {
		t.Fatal("a node outside the exhausted pool was resynced")
	}
	if sub, _ := s.GetActiveSubscription(ctx, u.ID); sub.TrafficExhausted {
		t.Fatal("one exhausted pool must not exhaust the whole period")
	}
	res = record(t, s, n2.ID, "b5", model.TrafficDelta{KeyUUID: k, Inbound: "a", BytesDown: 1 << 40})
	if len(res.Exhausted) != 0 {
		t.Fatalf("traffic outside pools has no limit: %+v", res.Exhausted)
	}
}

func TestPeriodExhaustedOnlyWhenEveryVisibleInboundIsSpent(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g")
	n := mkBareNode(t, s, "n1")
	x, y := mkInbound(t, s, n.ID, "x"), mkInbound(t, s, n.ID, "y")
	setMembers(t, s, g.ID, []int64{n.ID}, nil)
	p := mkGroupPlan(t, s, "p", g.ID, model.PlanPool{Name: "x", InboundIDs: []int64{x.ID}, TrafficLimitBytes: lim(100)})
	u, _ := userWithKey(t, s)
	period := pay(t, s, u.ID, p.ID, time.Now().UTC()).PeriodID
	setPoolUsed(t, s, period, p.Pools[0].ID, 100)
	if sub, _ := s.GetActiveSubscription(ctx, u.ID); sub.TrafficExhausted {
		t.Fatal("y is outside every pool and stays open")
	}
	p.Pools[0].InboundIDs = []int64{x.ID, y.ID}
	if err := s.UpdatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if sub, _ := s.GetActiveSubscription(ctx, u.ID); !sub.TrafficExhausted || !sub.MainIdle {
		t.Fatalf("every visible inbound sits in the spent pool: %+v", sub)
	}
}

func TestNotifyCandidatesCarryUsedPools(t *testing.T) {
	s := setupStore(t)
	now := time.Now()
	g := mkGroup(t, s, "g")
	n := mkBareNode(t, s, "n1")
	x := mkInbound(t, s, n.ID, "x")
	setMembers(t, s, g.ID, []int64{n.ID}, nil)
	p := mkGroupPlan(t, s, "premium", g.ID, model.PlanPool{Name: "Премиум", InboundIDs: []int64{x.ID}, TrafficLimitBytes: lim(100)})
	u := tgUser(t, s, 9201)
	period := mkPeriod(t, s, u.ID, p.ID, now.Add(-day), now.Add(20*day))
	setPoolUsed(t, s, period, p.Pools[0].ID, 90)
	c, ok := candidatesByUser(t, s, now)[u.ID]
	if !ok {
		t.Fatal("a pool at 90% must make the user a candidate")
	}
	if !c.MainIdle || len(c.Pools) != 1 || c.Pools[0].ID != p.Pools[0].ID || c.Pools[0].Name != "Премиум" ||
		c.Pools[0].Used != 90 || c.Pools[0].Limit == nil || *c.Pools[0].Limit != 100 || c.Exhausted {
		t.Fatalf("candidate = %+v", c)
	}
	setPoolUsed(t, s, period, p.Pools[0].ID, 100)
	if c := candidatesByUser(t, s, now)[u.ID]; !c.Exhausted {
		t.Fatalf("the only used pool is spent: %+v", c)
	}
}

func TestPoolBonusReopensOnlyThatPool(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g")
	n := mkBareNode(t, s, "n1")
	x, y := mkInbound(t, s, n.ID, "x"), mkInbound(t, s, n.ID, "y")
	setMembers(t, s, g.ID, []int64{n.ID}, nil)
	p := mkGroupPlan(t, s, "premium", g.ID,
		model.PlanPool{Name: "x", InboundIDs: []int64{x.ID}, TrafficLimitBytes: lim(100)},
		model.PlanPool{Name: "y", InboundIDs: []int64{y.ID}})
	u, k := userWithKey(t, s)
	period := pay(t, s, u.ID, p.ID, time.Now().UTC()).PeriodID
	setPoolUsed(t, s, period, p.Pools[0].ID, 100)
	if got := keyInbounds(t, s, n.ID, k); !slices.Equal(got, []string{"y"}) {
		t.Fatalf("spent pool open: %v", got)
	}
	before := nodeVersion(t, s, "n1")
	sub, err := s.AddTrafficBonus(ctx, u.ID, p.Pools[0].ID, 50, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if nodeVersion(t, s, "n1") == before {
		t.Fatal("a bonus must resync the nodes")
	}
	if sub.Pools[0].BonusBytes != 50 || *sub.Pools[0].TrafficLimitBytes != 150 || sub.Pools[0].Exhausted {
		t.Fatalf("after bonus: %+v", sub)
	}
	if got := keyInbounds(t, s, n.ID, k); !slices.Equal(got, []string{"x", "y"}) {
		t.Fatalf("bonus did not reopen the pool: %v", got)
	}
	if _, err := s.AddTrafficBonus(ctx, u.ID, p.Pools[1].ID, 1, time.Now().UTC()); !errors.Is(err, ErrUnlimitedPlan) {
		t.Fatalf("bonus to an unlimited pool: %v", err)
	}
	other := mkGroupPlan(t, s, "other", g.ID, model.PlanPool{Name: "x", InboundIDs: []int64{x.ID}, TrafficLimitBytes: lim(100)})
	if _, err := s.AddTrafficBonus(ctx, u.ID, other.Pools[0].ID, 1, time.Now().UTC()); !errors.Is(err, ErrPoolNotInPlan) {
		t.Fatalf("bonus to another plan's pool: %v", err)
	}
}

func TestPaymentStartsNowOnlyWhenEveryPoolIsSpent(t *testing.T) {
	s := setupStore(t)
	g := mkGroup(t, s, "g")
	n1, n2 := mkBareNode(t, s, "p1"), mkBareNode(t, s, "n2")
	x1, x2 := mkInbound(t, s, n1.ID, "x"), mkInbound(t, s, n2.ID, "x")
	setMembers(t, s, g.ID, []int64{n1.ID, n2.ID}, nil)
	p := mkGroupPlan(t, s, "premium", g.ID,
		model.PlanPool{Name: "Премиум", InboundIDs: []int64{x1.ID}, TrafficLimitBytes: lim(100)},
		model.PlanPool{Name: "Основной", InboundIDs: []int64{x2.ID}, TrafficLimitBytes: lim(1000)})
	u := mkUser(t, s)
	now := time.Now().UTC()
	period := pay(t, s, u.ID, p.ID, now).PeriodID
	setPoolUsed(t, s, period, p.Pools[1].ID, 1000)
	if res := pay(t, s, u.ID, p.ID, now); res.StartsNow {
		t.Fatal("the premium pool is still open: the new period must queue")
	}
	setPoolUsed(t, s, period, p.Pools[0].ID, 100)
	if res := pay(t, s, u.ID, p.ID, now); !res.StartsNow {
		t.Fatal("everything is spent: the new period must start now")
	}
}

func TestPoolIgnoresInboundsThePlanDoesNotSee(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g, other := mkGroup(t, s, "g"), mkGroup(t, s, "other")
	n, hidden := mkBareNode(t, s, "n1"), mkBareNode(t, s, "n2")
	x := mkInbound(t, s, n.ID, "x")
	h := mkInbound(t, s, hidden.ID, "x")
	setMembers(t, s, g.ID, []int64{n.ID}, nil)
	setMembers(t, s, other.ID, []int64{hidden.ID}, nil)
	p := mkGroupPlan(t, s, "own", g.ID,
		model.PlanPool{Name: "seen", InboundIDs: []int64{x.ID}, TrafficLimitBytes: lim(100)},
		model.PlanPool{Name: "hidden", InboundIDs: []int64{h.ID}, TrafficLimitBytes: lim(100)})
	u, k := userWithKey(t, s)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	if got := keyInbounds(t, s, hidden.ID, k); got != nil {
		t.Fatalf("a pool must not widen what the plan sees: %v", got)
	}
	sub, err := s.GetActiveSubscription(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.MainIdle || len(sub.Pools) != 1 || sub.Pools[0].PoolID != p.Pools[0].ID {
		t.Fatalf("main idle = %v, pools = %+v", sub.MainIdle, sub.Pools)
	}
	card, err := s.UserCard(ctx, u.ID)
	if err != nil || len(card.Periods) != 1 || !card.Periods[0].MainIdle || len(card.Periods[0].Pools) != 1 {
		t.Fatalf("card periods = %+v, %v", card.Periods, err)
	}
	rows, err := s.ListUsers(ctx, UserFilter{Number: &u.ID, Limit: 10})
	if err != nil || len(rows) != 1 || rows[0].TrafficLimitBytes == nil || *rows[0].TrafficLimitBytes != 100 {
		t.Fatalf("listed totals must count only the pool that covers something: %+v, %v", rows, err)
	}
	p.Pools[1].InboundIDs = nil
	if err := s.UpdatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupMembers(ctx, g.ID, []int64{n.ID, hidden.ID}, nil); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListUsers(ctx, UserFilter{Number: &u.ID, Limit: 10})
	if err != nil || len(rows) != 1 || rows[0].TrafficLimitBytes != nil {
		t.Fatalf("a visible inbound outside pools makes the total unlimited: %+v, %v", rows, err)
	}
}

func TestLegacyNodeKeysIgnoreXHTTPInbounds(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g")
	n := mkBareNode(t, s, "n1")
	a := mkInbound(t, s, n.ID, "a")
	cdn := mkInbound(t, s, n.ID, "cdn")
	if _, err := s.pool.Exec(ctx, `UPDATE node_inbounds SET transport = 'xhttp', security = 'tls' WHERE id = $1`, cdn.ID); err != nil {
		t.Fatal(err)
	}
	other := mkGroup(t, s, "other")
	setMembers(t, s, other.ID, nil, []int64{cdn.ID})
	setMembers(t, s, g.ID, nil, []int64{a.ID})
	p := mkGroupPlan(t, s, "p", g.ID)
	u, k := userWithKey(t, s)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	keys, err := s.ListNodeKeysLegacy(ctx, n.ID)
	if err != nil || len(keys) != 1 || keys[0].UUID != k {
		t.Fatalf("a key that sees every inbound an old agent gets was dropped: %+v, %v", keys, err)
	}
}
