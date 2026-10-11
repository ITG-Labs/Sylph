package service

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestSyncNodeUsers(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	g, err := svc.store.CreateLocationGroup(ctx, "premium")
	if err != nil {
		t.Fatal(err)
	}
	n := &model.Node{CertCN: "node-1", Name: "n1", GroupID: &g.ID}
	if err := svc.store.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	u1, key1, _ := svc.RegisterUser(ctx, 500)
	u2, key2, _ := svc.RegisterUser(ctx, 501)
	pGroup := &model.Plan{Name: "grp", PriceRub: 100, PeriodDays: 30, LocationGroupID: &g.ID}
	if err := svc.store.CreatePlan(ctx, pGroup); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RenewSubscription(ctx, u1.ID, pGroup.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	pAll := &model.Plan{Name: "all", PriceRub: 200, PeriodDays: 30}
	if err := svc.store.CreatePlan(ctx, pAll); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RenewSubscription(ctx, u2.ID, pAll.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.SyncNodeUsers(ctx, "node-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Version == 0 {
		t.Fatal("version should increment")
	}
	uuids := map[string]bool{}
	for _, u := range resp.Users {
		uuids[u.UUID] = true
	}
	if !uuids[key1] {
		t.Fatal("u1 key missing in sync")
	}
	if uuids[key2] {
		t.Fatal("u2 (no group) should not be in sync")
	}
	resp2, err := svc.SyncNodeUsers(ctx, "node-1", resp.Version)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Version != resp.Version || resp2.Users != nil {
		t.Fatalf("no-change sync: %+v", resp2)
	}
}

func TestSyncMarksLastSyncWithoutBump(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	if err := svc.store.UpsertNode(ctx, &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.SyncNodeUsers(ctx, "node-1", -1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.store.GetNodeByCN(ctx, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.LastSyncAt == nil {
		t.Fatal("last_sync_at not set by full sync")
	}
	if first.UsersVersion != resp.Version {
		t.Fatalf("version moved by sync mark: %d -> %d", resp.Version, first.UsersVersion)
	}
	if _, err := svc.SyncNodeUsers(ctx, "node-1", resp.Version); err != nil {
		t.Fatal(err)
	}
	second, err := svc.store.GetNodeByCN(ctx, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if second.LastSyncAt == nil || second.LastSyncAt.Before(*first.LastSyncAt) {
		t.Fatalf("no-change sync did not refresh last_sync_at: %v then %v", first.LastSyncAt, second.LastSyncAt)
	}
	if second.UsersVersion != resp.Version {
		t.Fatalf("version moved by no-change sync: %d -> %d", resp.Version, second.UsersVersion)
	}
}
