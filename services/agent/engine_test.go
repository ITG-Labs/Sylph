package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func testEngine(t *testing.T, fx *fakeXray, fc *fakeCore) *Engine {
	t.Helper()
	ts := httptestOf(fc, t)
	cfg := &Config{CoreURL: ts.URL, InboundTag: "vless-in", CachePath: filepath.Join(t.TempDir(), "c.json")}
	return NewEngine(cfg, newCoreClientForTest(cfg, discardLogger()), fx, discardLogger())
}

func deadEngine(t *testing.T, fx *fakeXray, cachePath string) *Engine {
	t.Helper()
	cfg := &Config{CoreURL: deadCoreURL(t), InboundTag: "vless-in", CachePath: cachePath}
	return NewEngine(cfg, newCoreClientForTest(cfg, discardLogger()), fx, discardLogger())
}

func users(ids ...string) []SyncUser {
	out := make([]SyncUser, 0, len(ids))
	for _, id := range ids {
		out = append(out, SyncUser{UUID: id})
	}
	return out
}

func TestReconcileAddsDesired(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	e.setDesired(users("a", "b"))
	if err := e.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fx.userCount() != 2 {
		t.Fatalf("users = %d", fx.userCount())
	}
}

func TestReconcileRemovesMissing(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	e.setDesired(users("a", "b"))
	if err := e.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	e.setDesired(users("b", "c"))
	if err := e.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("vless-in", "a") || !fx.hasUser("vless-in", "c") || fx.userCount() != 2 {
		t.Fatalf("want {b,c}, got count %d", fx.userCount())
	}
}

func TestReconcileNoChangeDoesNotTouchXray(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	e.setDesired(users("a"))
	if err := e.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	fx.set(func(f *fakeXray) { f.addErr = errors.New("must not be called") })
	if err := e.Reconcile(ctx); err != nil {
		t.Fatalf("unchanged set touched xray: %v", err)
	}
}

func TestReconcileXrayFailureNoCrash(t *testing.T) {
	fx := newFakeXray()
	fx.addErr = errors.New("xray down")
	e := testEngine(t, fx, &fakeCore{})
	e.setDesired(users("a"))
	if err := e.Reconcile(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if fx.userCount() != 0 {
		t.Fatalf("xray state = %d", fx.userCount())
	}
}

func TestNoDesiredStateDoesNotWipeXray(t *testing.T) {
	fx := newFakeXray()
	fx.users["vless-in/live"] = true
	e := deadEngine(t, fx, filepath.Join(t.TempDir(), "missing.json"))
	if err := e.LoadState(e.cfg.CachePath); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("vless-in", "live") {
		t.Fatal("unknown desired state wiped live xray users")
	}
}

func TestXrayRestartReappliesUsers(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	e.setDesired(users("a", "b"))
	if err := e.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	fx.wipe()
	if err := e.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if fx.userCount() != 2 {
		t.Fatalf("users after xray restart = %d, want 2", fx.userCount())
	}
}

func TestLateXrayWithCoreDownServesCachedUsers(t *testing.T) {
	fx := newFakeXray()
	cachePath := filepath.Join(t.TempDir(), "c.json")
	if err := SaveCache(cachePath, &Cache{Version: 4, Users: users("cached")}); err != nil {
		t.Fatal(err)
	}
	fx.listErr = errors.New("connection refused")
	e := deadEngine(t, fx, cachePath)
	if err := e.LoadState(cachePath); err == nil {
		t.Fatal("want error while xray is not ready")
	}
	fx.set(func(f *fakeXray) { f.listErr = nil })
	if err := e.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("vless-in", "cached") {
		t.Fatal("cached user never applied after xray came up")
	}
}

func TestAgentRestartRemovesRevokedLeftover(t *testing.T) {
	fx := newFakeXray()
	cachePath := filepath.Join(t.TempDir(), "c.json")
	fc := &fakeCore{syncVersion: 2, syncUsers: users("a", "c", "d")}
	fx.failAdd = map[string]bool{"d": true}
	first := testEngine(t, fx, fc)
	first.cfg.CachePath = cachePath
	ctx := context.Background()
	if _, err := first.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.Reconcile(ctx); err == nil {
		t.Fatal("want error from d")
	}
	if err := SaveCache(cachePath, &Cache{Version: 3, Users: users("a")}); err != nil {
		t.Fatal(err)
	}
	fx.set(func(f *fakeXray) { f.failAdd = nil })
	second := deadEngine(t, fx, cachePath)
	if err := second.LoadState(cachePath); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("vless-in", "c") {
		t.Fatal("revoked c kept access across agent restart")
	}
}

func TestRemoveFailureRetriedNextTick(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	e.setDesired(users("x"))
	if err := e.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	fx.set(func(f *fakeXray) { f.failRemove = map[string]bool{"x": true} })
	e.setDesired(nil)
	if err := e.Reconcile(ctx); err == nil {
		t.Fatal("want error from failed remove")
	}
	fx.set(func(f *fakeXray) { f.failRemove = nil })
	if err := e.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("vless-in", "x") {
		t.Fatal("revoked x still in xray after retry")
	}
}

func TestFreshAgentGetsListAtVersionZero(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 0, syncUsers: users("u")}
	e := testEngine(t, fx, fc)
	ctx := context.Background()
	if _, err := e.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("vless-in", "u") {
		t.Fatal("fresh agent got no users from node at version 0")
	}
}

func TestSyncSavesCacheEvenIfApplyFails(t *testing.T) {
	fx := newFakeXray()
	fx.failAdd = map[string]bool{"bad": true}
	fc := &fakeCore{syncVersion: 7, syncUsers: users("ok", "bad")}
	e := testEngine(t, fx, fc)
	ctx := context.Background()
	if _, err := e.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(ctx); err == nil {
		t.Fatal("want error from bad")
	}
	cached, err := LoadCache(e.cfg.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Version != 7 || len(cached.Users) != 2 {
		t.Fatalf("cache not updated to last valid list: %+v", cached)
	}
}

func TestSyncOnceNoChangeKeepsDesired(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 3, syncUsers: users("x")}
	e := testEngine(t, fx, fc)
	ctx := context.Background()
	if _, err := e.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	resp, err := e.SyncOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Users != nil {
		t.Fatalf("no-change sync returned users: %+v", resp.Users)
	}
	if !e.ActiveUsers()["x"] {
		t.Fatal("desired state lost on no-change sync")
	}
}

func TestSyncOnceCoreDownKeepsXray(t *testing.T) {
	fx := newFakeXray()
	e := deadEngine(t, fx, filepath.Join(t.TempDir(), "c.json"))
	if _, err := e.SyncOnce(context.Background()); err == nil {
		t.Fatal("core down should error")
	}
	if fx.userCount() != 0 {
		t.Fatalf("xray touched: %d", fx.userCount())
	}
}

func TestLoadStateAppliesCache(t *testing.T) {
	fx := newFakeXray()
	cachePath := filepath.Join(t.TempDir(), "c.json")
	if err := SaveCache(cachePath, &Cache{Version: 9, Users: users("cached-user")}); err != nil {
		t.Fatal(err)
	}
	e := deadEngine(t, fx, cachePath)
	if err := e.LoadState(cachePath); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("vless-in", "cached-user") {
		t.Fatal("cached user not applied")
	}
}

func TestKeyRefIsStableAndOpaque(t *testing.T) {
	uuid := "4f1d2c3b-aaaa-4bbb-8ccc-0123456789ab"
	ref := keyRef(uuid)
	if len(ref) != 12 || ref != keyRef(uuid) || strings.Contains(uuid, ref) {
		t.Fatalf("bad key ref %q", ref)
	}
}

func TestReconcileLogsDiffWithoutKeys(t *testing.T) {
	var buf bytes.Buffer
	fx := newFakeXray()
	added := "4f1d2c3b-aaaa-4bbb-8ccc-0123456789ab"
	failing := "9e8d7c6b-bbbb-4ccc-9ddd-ba9876543210"
	fx.failAdd = map[string]bool{failing: true}
	ts := httptestOf(&fakeCore{}, t)
	cfg := &Config{CoreURL: ts.URL, InboundTag: "vless-in", CachePath: filepath.Join(t.TempDir(), "c.json")}
	e := NewEngine(cfg, newCoreClientForTest(cfg, discardLogger()), fx, slog.New(slog.NewJSONHandler(&buf, nil)))
	e.setDesired(users(added, failing))
	err := e.Reconcile(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	logs := buf.String()
	for _, secret := range []string{added, failing} {
		if strings.Contains(logs, secret) || strings.Contains(err.Error(), secret) {
			t.Fatalf("key leaked: logs=%s err=%v", logs, err)
		}
	}
	if !strings.Contains(logs, "xray_user_added") || !strings.Contains(logs, keyRef(added)) {
		t.Fatalf("applied diff not logged: %s", logs)
	}
	if !strings.Contains(logs, keyRef(failing)) {
		t.Fatalf("failure lacks key ref: %s", logs)
	}
}
