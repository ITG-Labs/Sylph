package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func poolNode(t *testing.T, svc *Service, cn string) *model.Node {
	t.Helper()
	n := &model.Node{CertCN: cn, Name: cn, Address: cn + ".example.com", Enabled: true}
	if err := svc.CreateNode(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	spec := inbound.Spec{Tag: "main", Port: 443, Reality: &inbound.Reality{Dest: "www.example.org:443", ServerNames: []string{"www." + cn + ".example.org"}}}
	if _, err := svc.CreateInbound(context.Background(), n.ID, spec); err != nil {
		t.Fatal(err)
	}
	return n
}

func wantField(t *testing.T, err error, field string) {
	t.Helper()
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Fatalf("err = %v, want a field error on %q", err, field)
	}
}

func TestPlanNeedsAGroup(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	p := &model.Plan{Name: "m", PriceRub: 1, PeriodDays: 30, Visibility: model.VisibilityPublic}
	wantField(t, svc.CreatePlanChecked(ctx, p), "location_group_id")
	missing := int64(999999)
	p.LocationGroupID = &missing
	wantField(t, svc.CreatePlanChecked(ctx, p), "location_group_id")
	p.LocationGroupID = testGroup(t, svc.store)
	if err := svc.CreatePlanChecked(ctx, p); err != nil {
		t.Fatal(err)
	}
	_, err := svc.UpdatePlan(ctx, p.ID, PlanPatch{LocationGroupID: Optional[int64]{Set: true}})
	wantField(t, err, "location_group_id")
}

func TestPlanPoolsAreValidated(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	n := poolNode(t, svc, "n1")
	group := testGroup(t, svc.store)
	p := &model.Plan{Name: "m", PriceRub: 1, PeriodDays: 30, Visibility: model.VisibilityPublic, LocationGroupID: group}
	if err := svc.CreatePlanChecked(ctx, p); err != nil {
		t.Fatal(err)
	}
	zero, missing := int64(0), int64(999999)
	long := string(make([]rune, 65))
	for _, pools := range [][]model.PlanPool{
		{{}},
		{{NodeID: &n.ID, GroupID: group}},
		{{NodeID: &n.ID, TrafficLimitBytes: &zero}},
		{{NodeID: &missing}},
		{{GroupID: &missing}},
		{{ID: missing, NodeID: &n.ID}},
		{{NodeID: &n.ID, Name: long}},
	} {
		_, err := svc.UpdatePlan(ctx, p.ID, PlanPatch{Pools: &pools})
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "pools" {
			t.Fatalf("pools %+v: err = %v, want a field error on pools", pools, err)
		}
	}
	if err := svc.SetGroupMembers(ctx, *group, []int64{n.ID}, nil); err != nil {
		t.Fatal(err)
	}
	overlap := []model.PlanPool{{NodeID: &n.ID}, {GroupID: group}}
	_, err := svc.UpdatePlan(ctx, p.ID, PlanPatch{Pools: &overlap})
	wantField(t, err, "pools")
	limit := int64(100)
	good := []model.PlanPool{{Name: "Премиум", NodeID: &n.ID, TrafficLimitBytes: &limit}}
	got, err := svc.UpdatePlan(ctx, p.ID, PlanPatch{Pools: &good})
	if err != nil || len(got.Pools) != 1 || got.Pools[0].ID == 0 || got.Pools[0].Name != "Премиум" {
		t.Fatalf("pools = %+v, %v", got, err)
	}
	renamed, err := svc.UpdatePlan(ctx, p.ID, PlanPatch{Name: new("renamed")})
	if err != nil || len(renamed.Pools) != 1 || renamed.Pools[0].ID != got.Pools[0].ID {
		t.Fatalf("a patch without pools must keep them: %+v, %v", renamed, err)
	}
	var patch PlanPatch
	if err := json.Unmarshal([]byte(`{"pools":[]}`), &patch); err != nil {
		t.Fatal(err)
	}
	cleared, err := svc.UpdatePlan(ctx, p.ID, patch)
	if err != nil || len(cleared.Pools) != 0 {
		t.Fatalf("cleared = %+v, %v", cleared, err)
	}
}

func TestNodeGroupIDs(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	g1, g2 := testGroup(t, svc.store), testGroup(t, svc.store)
	n := &model.Node{CertCN: "n1", Name: "n1", Address: "n1.example.com", Enabled: true, GroupIDs: []int64{*g1}}
	if err := svc.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetNode(ctx, n.ID)
	if err != nil || !slices.Equal(got.GroupIDs, []int64{*g1}) {
		t.Fatalf("created = %+v, %v", got, err)
	}
	updated, err := svc.UpdateNode(ctx, n.ID, NodePatch{GroupIDs: &[]int64{*g2}})
	if err != nil || !slices.Equal(updated.GroupIDs, []int64{*g2}) {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	renamed, err := svc.UpdateNode(ctx, n.ID, NodePatch{Name: new("renamed")})
	if err != nil || !slices.Equal(renamed.GroupIDs, []int64{*g2}) {
		t.Fatalf("a patch without groups must keep them: %+v, %v", renamed, err)
	}
	_, err = svc.UpdateNode(ctx, n.ID, NodePatch{GroupIDs: &[]int64{999999}})
	wantField(t, err, "group_ids")
	bad := &model.Node{CertCN: "n2", Name: "n2", Address: "n2.example.com", GroupIDs: []int64{999999}}
	wantField(t, svc.CreateNode(ctx, bad), "group_ids")
	if _, err := svc.store.GetNodeByCN(ctx, "n2"); err == nil {
		t.Fatal("a node with a bad group was created")
	}
}

func TestGroupMembersAndDelete(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	g, err := svc.CreateGroup(ctx, "eu")
	if err != nil {
		t.Fatal(err)
	}
	n := poolNode(t, svc, "n1")
	if err := svc.SetGroupMembers(ctx, g.ID, nil, []int64{999999}); err == nil {
		t.Fatal("unknown inbound accepted")
	} else {
		wantField(t, err, "members")
	}
	if err := svc.SetGroupMembers(ctx, 999999, nil, nil); err == nil {
		t.Fatal("unknown group accepted")
	}
	if err := svc.SetGroupMembers(ctx, g.ID, []int64{n.ID}, nil); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if err := svc.DeleteGroup(ctx, g.ID); !errors.As(err, &ce) {
		t.Fatalf("delete a group with members: %v", err)
	}
	if err := svc.SetGroupMembers(ctx, g.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteGroup(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
}
