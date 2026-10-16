package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func cascadeDoc(to ...int64) json.RawMessage {
	outbounds := []map[string]any{{"tag": "direct", "kind": "direct"}}
	for i, id := range to {
		outbounds = append(outbounds, map[string]any{"tag": "c" + string(rune('a'+i)), "kind": "cascade", "cascade": map[string]any{"node_id": id}})
	}
	doc, _ := json.Marshal(map[string]any{"outbounds": outbounds, "default": "direct"})
	return doc
}

func mkProfile(t *testing.T, s *Store, name string, doc json.RawMessage) *ProfileRow {
	t.Helper()
	p, err := s.CreateProfile(context.Background(), name, doc)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func setProfile(t *testing.T, s *Store, n *model.Node, profileID *int64) {
	t.Helper()
	n.ProfileID = profileID
	if err := s.UpdateNode(context.Background(), n); err != nil {
		t.Fatal(err)
	}
}

func TestProfileRevisionsAndNames(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	p := mkProfile(t, s, "base", cascadeDoc())
	if p.ID == 0 || p.Revision != 1 || p.Name != "base" {
		t.Fatalf("created = %+v", p)
	}
	if _, err := s.CreateProfile(ctx, "base", cascadeDoc()); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	got, err := s.UpdateProfile(ctx, p.ID, "base-2", json.RawMessage(`{"default":"x"}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || got.Name != "base-2" || string(got.Doc) != `{"default": "x"}` {
		t.Fatalf("updated = %+v %s", got, got.Doc)
	}
	if _, err := s.UpdateProfile(ctx, p.ID, "base-3", cascadeDoc(), 1); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale revision: %v", err)
	}
	if _, err := s.UpdateProfile(ctx, 999, "x", cascadeDoc(), 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	n := deltaNode(t, s)
	setProfile(t, s, n, &p.ID)
	list, err := s.ListProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Nodes != 1 || list[0].Name != "base-2" {
		t.Fatalf("list = %+v", list)
	}
	again, err := s.GetProfile(ctx, p.ID)
	if err != nil || again.Revision != 2 {
		t.Fatalf("get = %+v %v", again, err)
	}
}

func TestProfileUpdateBumpsOnlyItsNodes(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	p := mkProfile(t, s, "p", cascadeDoc())
	with, without := deltaNode(t, s), deltaNode(t, s)
	setProfile(t, s, with, &p.ID)
	vw, vo := nodeVersion(t, s, with.CertCN), nodeVersion(t, s, without.CertCN)
	if _, err := s.UpdateProfile(ctx, p.ID, "p", json.RawMessage(`{"default":"direct","outbounds":[]}`), 1); err != nil {
		t.Fatal(err)
	}
	if nodeVersion(t, s, with.CertCN) <= vw || nodeVersion(t, s, without.CertCN) != vo {
		t.Fatal("profile update bumped the wrong nodes")
	}
	before := nodeVersion(t, s, without.CertCN)
	setProfile(t, s, without, &p.ID)
	if nodeVersion(t, s, without.CertCN) <= before {
		t.Fatal("assigning a profile did not bump the node")
	}
}

func TestDeleteOnlyUnusedProfile(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	p := mkProfile(t, s, "p", cascadeDoc())
	n := deltaNode(t, s)
	setProfile(t, s, n, &p.ID)
	if err := s.DeleteProfile(ctx, p.ID); !errors.Is(err, ErrProfileInUse) {
		t.Fatalf("delete in use: %v", err)
	}
	setProfile(t, s, n, nil)
	if err := s.DeleteProfile(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProfile(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestRefreshLinksFollowsProfiles(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	x, y, z := deltaNode(t, s), deltaNode(t, s), deltaNode(t, s)
	p := mkProfile(t, s, "bridge", cascadeDoc(y.ID))
	setProfile(t, s, x, &p.ID)
	setProfile(t, s, y, &p.ID)
	vy, vz := nodeVersion(t, s, y.CertCN), nodeVersion(t, s, z.CertCN)
	if err := s.RefreshLinks(ctx); err != nil {
		t.Fatal(err)
	}
	keys, err := s.ServiceKeys(ctx, y.ID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.LinkKey(ctx, x.ID, y.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != key || len(key) != 36 {
		t.Fatalf("service keys = %v, link key %q", keys, key)
	}
	if _, err := s.LinkKey(ctx, y.ID, y.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("self link: %v", err)
	}
	if nodeVersion(t, s, y.CertCN) <= vy || nodeVersion(t, s, z.CertCN) != vz {
		t.Fatal("a new link did not bump only the exit node")
	}
	vx := nodeVersion(t, s, x.CertCN)
	if err := s.BumpNodesCascadingTo(ctx, y.ID); err != nil {
		t.Fatal(err)
	}
	if nodeVersion(t, s, x.CertCN) <= vx {
		t.Fatal("cascading node not bumped")
	}
	vy = nodeVersion(t, s, y.CertCN)
	if err := s.RefreshLinks(ctx); err != nil {
		t.Fatal(err)
	}
	if nodeVersion(t, s, y.CertCN) != vy {
		t.Fatal("refresh without changes bumped the exit node")
	}
	if _, err := s.UpdateProfile(ctx, p.ID, "bridge", cascadeDoc(), 1); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshLinks(ctx); err != nil {
		t.Fatal(err)
	}
	if keys, _ := s.ServiceKeys(ctx, y.ID); len(keys) != 0 {
		t.Fatalf("stale link kept: %v", keys)
	}
	if nodeVersion(t, s, y.CertCN) <= vy {
		t.Fatal("removing a link did not bump the exit node")
	}
}

func TestNodeUserKeysCarryThePlan(t *testing.T) {
	s := setupStore(t)
	u, k := userWithKey(t, s)
	p := mkPlan(t, s)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	n := deltaNode(t, s)
	keys, err := s.ListNodeKeys(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].UUID != k || keys[0].PlanID != p.ID {
		t.Fatalf("keys = %+v", keys)
	}
}

func TestGeoSources(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g, err := s.CreateGeoSource(ctx, GeoSource{Name: "ru-blocked", Kind: "domain", URL: "https://example.com/geosite.dat", SHA256URL: "https://example.com/geosite.dat.sha256sum"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGeoSource(ctx, GeoSource{Name: "ru-blocked", Kind: "ip", URL: "https://example.com/geoip.dat"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	g.URL = "https://mirror.example.com/geosite.dat"
	upd, err := s.UpdateGeoSource(ctx, *g)
	if err != nil || upd.URL != g.URL || !upd.UpdatedAt.After(g.CreatedAt.Add(-time.Second)) {
		t.Fatalf("update = %+v %v", upd, err)
	}
	list, err := s.ListGeoSources(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "ru-blocked" {
		t.Fatalf("list = %+v %v", list, err)
	}
	if err := s.DeleteGeoSource(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGeoSource(ctx, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestGeoRefreshBumpsNodesWithProfiles(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	p := mkProfile(t, s, "p", cascadeDoc())
	with, without := deltaNode(t, s), deltaNode(t, s)
	setProfile(t, s, with, &p.ID)
	vw, vo := nodeVersion(t, s, with.CertCN), nodeVersion(t, s, without.CertCN)
	gen, err := s.RequestGeoRefresh(ctx)
	if err != nil || gen != 1 {
		t.Fatalf("generation = %d %v", gen, err)
	}
	if nodeVersion(t, s, with.CertCN) <= vw || nodeVersion(t, s, without.CertCN) != vo {
		t.Fatal("geo refresh bumped the wrong nodes")
	}
	st, err := s.GetSettings(ctx)
	if err != nil || st.GeoGeneration != 1 {
		t.Fatalf("settings = %+v %v", st, err)
	}
	vw = nodeVersion(t, s, with.CertCN)
	if err := s.BumpNodes(ctx, []int64{with.ID}); err != nil {
		t.Fatal(err)
	}
	if nodeVersion(t, s, with.CertCN) <= vw {
		t.Fatal("explicit bump did nothing")
	}
}

func TestHeartbeatKeepsGeoStatusAndRoutingError(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := deltaNode(t, s)
	msg := "AddRule: bad"
	v := nodeVersion(t, s, n.CertCN)
	if err := s.TouchNodeHeartbeat(ctx, n.ID, model.NodeHeartbeat{AgentVersion: "1", GeoStatus: json.RawMessage(`{"ru-blocked":{"sha256":"ab"}}`), RoutingError: &msg}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNode(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RoutingError == nil || *got.RoutingError != msg || !slices.Contains([]string{`{"ru-blocked": {"sha256": "ab"}}`}, string(got.GeoStatus)) {
		t.Fatalf("node = %+v %s", got, got.GeoStatus)
	}
	if got.UsersVersion != v {
		t.Fatal("heartbeat bumped the node")
	}
}
