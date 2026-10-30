package agent

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func routingSyncExtra() map[string]any {
	return map[string]any{
		"routing": map[string]any{
			"outbounds": []any{map[string]any{"tag": "direct", "kind": "direct", "direct": map[string]any{"ip": "auto"}}, map[string]any{"tag": "block", "kind": "block"}},
			"rules":     []any{map[string]any{"plans": []int64{3}, "to": "block"}},
			"default":   "direct",
		},
		"service_keys": []string{"svc-1"},
		"geo":          map[string]any{"generation": 2, "sources": []any{map[string]any{"name": "ru", "kind": "domain", "url": "https://example.com/ru.dat"}}},
	}
}

func TestSyncKeepsRoutingServiceKeysAndPlans(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 4, syncUsers: []SyncUser{{UUID: "a", PlanID: 3}, {UUID: "b"}}, syncInbounds: testInbounds("main"), syncExtra: routingSyncExtra()}
	e := testEngine(t, fx, fc)
	if _, err := e.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	active := e.ActiveUsers()
	if !active["a"] || !active["b"] || !active["svc-1"] || len(active) != 3 {
		t.Fatalf("active = %v", active)
	}
	d := e.Desired(nil)
	if d.Routing == nil || d.Routing.Default != "direct" || !reflect.DeepEqual(d.ServiceKeys, []string{"svc-1"}) || d.Geo == nil || d.Geo.Generation != 2 || len(d.Geo.Sources) != 1 {
		t.Fatalf("desired = %+v", d)
	}
	if got := e.PlanUsers(); !reflect.DeepEqual(got, map[int64][]string{3: {"a@main"}}) {
		t.Fatalf("plan users = %v", got)
	}
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if !fx.hasEmail("main", "svc-1") || fx.hasUser("main", "svc-1") {
		t.Fatal("service key is not let into the inbound")
	}
	restored := deadEngine(t, newFakeXray(), filepath.Join(filepath.Dir(e.cfg.CachePath), "c.json"))
	if err := restored.LoadState(restored.cfg.CachePath); err != nil {
		t.Fatal(err)
	}
	if rd := restored.Desired(nil); rd.Routing == nil || len(rd.ServiceKeys) != 1 || rd.Geo == nil || !restored.ActiveUsers()["svc-1"] {
		t.Fatalf("restored = %+v", rd)
	}
	if got := restored.PlanUsers(); !reflect.DeepEqual(got, map[int64][]string{3: {"a@main"}}) {
		t.Fatalf("restored plan users = %v", got)
	}
}
