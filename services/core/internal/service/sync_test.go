package service

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestSyncNodeUsers(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	g, err := svc.store.CreateLocationGroup(ctx, "premium")
	if err != nil {
		t.Fatal(err)
	}
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := svc.store.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetNodeGroups(ctx, n.ID, []int64{g.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateInbound(ctx, n.ID, inbound.Spec{Tag: "main", Port: 443, Reality: &inbound.Reality{Dest: "www.example.org:443", ServerNames: []string{"www.example.org"}}}); err != nil {
		t.Fatal(err)
	}
	u1, key1, _ := svc.RegisterUser(ctx, 500)
	u2, key2, _ := svc.RegisterUser(ctx, 501)
	pGroup := &model.Plan{Name: "grp", PriceRub: 100, PeriodDays: 30, LocationGroupID: &g.ID}
	if err := svc.store.CreatePlan(ctx, pGroup); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.ApplyPayment(ctx, u1.ID, pGroup.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	pAll := &model.Plan{Name: "all", PriceRub: 200, PeriodDays: 30, LocationGroupID: testGroup(t, svc.store)}
	if err := svc.store.CreatePlan(ctx, pAll); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.ApplyPayment(ctx, u2.ID, pAll.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.SyncNodeUsers(ctx, "node-1", 0, AgentCaps{PerInbound: true})
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
	resp2, err := svc.SyncNodeUsers(ctx, "node-1", resp.Version, AgentCaps{PerInbound: true})
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
	resp, err := svc.SyncNodeUsers(ctx, "node-1", -1, AgentCaps{PerInbound: true})
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
	if _, err := svc.SyncNodeUsers(ctx, "node-1", resp.Version, AgentCaps{PerInbound: true}); err != nil {
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

func addReality(t *testing.T, svc *Service, nodeID int64, tag, sni string) {
	t.Helper()
	spec := inbound.Spec{Tag: tag, Port: 443, Label: tag, Reality: &inbound.Reality{Dest: sni + ":443", ServerNames: []string{sni}}}
	if _, err := svc.CreateInbound(context.Background(), nodeID, spec); err != nil {
		t.Fatal(err)
	}
}

func TestSyncReturnsInbounds(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1", Address: "n1.example.com", Enabled: true}
	if err := svc.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	empty, err := svc.SyncNodeUsers(ctx, "node-1", -1, AgentCaps{PerInbound: true})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Inbounds == nil || len(empty.Inbounds) != 0 {
		t.Fatalf("full sync without inbounds must carry an empty list, got %#v", empty.Inbounds)
	}
	addReality(t, svc, n.ID, "b-second", "www.second.com")
	addReality(t, svc, n.ID, "a-first", "www.first.com")
	resp, err := svc.SyncNodeUsers(ctx, "node-1", empty.Version, AgentCaps{PerInbound: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Version == empty.Version {
		t.Fatal("inbound changes must bump the node version")
	}
	if len(resp.Inbounds) != 2 || resp.Inbounds[0].Tag != "a-first" || resp.Inbounds[1].Tag != "b-second" {
		t.Fatalf("inbounds = %+v", resp.Inbounds)
	}
	a := resp.Inbounds[0]
	if a.Port != 443 || a.Protocol != "vless" || a.Transport != "tcp" || a.Security != "reality" || a.Flow != "xtls-rprx-vision" {
		t.Fatalf("inbound = %+v", a)
	}
	if a.Reality == nil || a.Reality.PrivateKey == "" || a.Reality.Dest != "www.first.com:443" ||
		len(a.Reality.ServerNames) != 1 || len(a.Reality.ShortIDs) != 1 || a.Reality.Fingerprint != "chrome" {
		t.Fatalf("reality = %+v", a.Reality)
	}
	same, err := svc.SyncNodeUsers(ctx, "node-1", resp.Version, AgentCaps{PerInbound: true})
	if err != nil {
		t.Fatal(err)
	}
	if same.Inbounds != nil || same.Users != nil {
		t.Fatalf("no-change sync must not carry inbounds or users: %+v", same)
	}
}

func TestSyncInboundsAreNodeScoped(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	n1 := &model.Node{CertCN: "node-1", Name: "n1", Address: "n1.example.com", Enabled: true}
	n2 := &model.Node{CertCN: "node-2", Name: "n2", Address: "n2.example.com", Enabled: true}
	for _, n := range []*model.Node{n1, n2} {
		if err := svc.CreateNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	addReality(t, svc, n1.ID, "main", "www.one.com")
	resp, err := svc.SyncNodeUsers(ctx, "node-2", -1, AgentCaps{PerInbound: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Inbounds) != 0 {
		t.Fatalf("node-2 got node-1 inbounds: %+v", resp.Inbounds)
	}
}
