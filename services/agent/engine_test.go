package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testEngine(t *testing.T, fx *fakeXray, fc *fakeCore) *Engine {
	t.Helper()
	ts := httptestOf(fc, t)
	cfg := &Config{CoreURL: ts.URL, CachePath: filepath.Join(t.TempDir(), "c.json")}
	return NewEngine(cfg, newCoreClientForTest(cfg, discardLogger()), fx, discardLogger())
}

func deadEngine(t *testing.T, fx *fakeXray, cachePath string) *Engine {
	t.Helper()
	cfg := &Config{CoreURL: deadCoreURL(t), CachePath: cachePath}
	return NewEngine(cfg, newCoreClientForTest(cfg, discardLogger()), fx, discardLogger())
}

func users(ids ...string) []SyncUser {
	out := make([]SyncUser, 0, len(ids))
	for _, id := range ids {
		out = append(out, SyncUser{UUID: id})
	}
	return out
}

var mainIn = testInbounds("main")

func TestReconcileAddsDesired(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	e.setDesired(users("a", "b"))
	if err := e.Reconcile(context.Background(), mainIn); err != nil {
		t.Fatal(err)
	}
	if fx.userCount() != 2 || !fx.hasUser("main", "a") || !fx.hasUser("main", "b") {
		t.Fatalf("users = %d", fx.userCount())
	}
}

func TestReconcilePutsEveryUserIntoEveryInboundWithItsFlow(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	plain := testInbound("plain")
	plain.Flow = ""
	inbounds := []Inbound{testInbound("main"), plain}
	e.setDesired(users("a", "b"))
	if err := e.Reconcile(context.Background(), inbounds); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"a", "b"} {
		if !fx.hasUser("main", u) || fx.flowOf("main", u) != "xtls-rprx-vision" {
			t.Fatalf("user %s in main: present %v flow %q", u, fx.hasUser("main", u), fx.flowOf("main", u))
		}
		if !fx.hasUser("plain", u) || fx.flowOf("plain", u) != "" {
			t.Fatalf("user %s in plain: present %v flow %q", u, fx.hasUser("plain", u), fx.flowOf("plain", u))
		}
	}
	if fx.userCount() != 4 {
		t.Fatalf("user entries = %d, want 2 users x 2 inbounds", fx.userCount())
	}
}

func TestReconcileRemovesMissing(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	inbounds := testInbounds("alpha", "beta")
	e.setDesired(users("a", "b"))
	if err := e.Reconcile(ctx, inbounds); err != nil {
		t.Fatal(err)
	}
	e.setDesired(users("b", "c"))
	if err := e.Reconcile(ctx, inbounds); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"alpha", "beta"} {
		if fx.hasUser(tag, "a") || !fx.hasUser(tag, "b") || !fx.hasUser(tag, "c") {
			t.Fatalf("inbound %s: want {b,c}", tag)
		}
	}
	if fx.userCount() != 4 {
		t.Fatalf("user entries = %d, want 4", fx.userCount())
	}
}

func TestReconcileNoChangeDoesNotTouchXray(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	e.setDesired(users("a"))
	if err := e.Reconcile(ctx, mainIn); err != nil {
		t.Fatal(err)
	}
	fx.set(func(f *fakeXray) { f.addErr = errors.New("must not be called") })
	if err := e.Reconcile(ctx, mainIn); err != nil {
		t.Fatalf("unchanged set touched xray: %v", err)
	}
}

func TestReconcileXrayFailureNoCrash(t *testing.T) {
	fx := newFakeXray()
	fx.addErr = errors.New("xray down")
	e := testEngine(t, fx, &fakeCore{})
	e.setDesired(users("a"))
	if err := e.Reconcile(context.Background(), mainIn); err == nil {
		t.Fatal("want error")
	}
	if fx.userCount() != 0 {
		t.Fatalf("xray state = %d", fx.userCount())
	}
}

func TestReconcileUnknownInboundsTouchesNothing(t *testing.T) {
	fx := newFakeXray()
	fx.users["main/live"] = true
	e := testEngine(t, fx, &fakeCore{})
	e.setDesired(users("a"))
	if err := e.Reconcile(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("main", "live") || fx.userCount() != 1 {
		t.Fatal("unknown inbounds must leave xray alone")
	}
}

func TestReconcileStopsOnShutdown(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	e.setDesired(users("a", "b", "c"))
	ctx, cancel := context.WithCancel(context.Background())
	fx.afterAdd = cancel
	err := e.Reconcile(ctx, testInbounds("alpha", "beta"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if fx.userCount() != 1 {
		t.Fatalf("users added after shutdown: %d, want only the one before SIGTERM", fx.userCount())
	}
}

func TestReconcileStopsOnShutdownRemoving(t *testing.T) {
	fx := newFakeXray()
	for _, u := range []string{"a", "b", "c"} {
		fx.users["main/"+u] = true
	}
	e := testEngine(t, fx, &fakeCore{})
	e.setDesired(nil)
	ctx, cancel := context.WithCancel(context.Background())
	fx.afterRemove = cancel
	if err := e.Reconcile(ctx, mainIn); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if fx.userCount() != 2 {
		t.Fatalf("users left = %d, want 2: removal must stop at SIGTERM", fx.userCount())
	}
}

func TestRevokeOnlyRemoves(t *testing.T) {
	fx := newFakeXray()
	fx.users["vless-in/a"] = true
	fx.users["vless-in/revoked"] = true
	e := testEngine(t, fx, &fakeCore{})
	e.setDesired(users("a", "new"))
	if err := e.Revoke(context.Background(), []string{"vless-in"}); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("vless-in", "revoked") || !fx.hasUser("vless-in", "a") {
		t.Fatal("revoke must remove exactly the users that lost access")
	}
	if fx.hasUser("vless-in", "new") {
		t.Fatal("revoke must not add users: their flow on that inbound is unknown")
	}
}

func TestRevokeWithoutDesiredStateTouchesNothing(t *testing.T) {
	fx := newFakeXray()
	fx.users["vless-in/live"] = true
	e := testEngine(t, fx, &fakeCore{})
	if err := e.Revoke(context.Background(), []string{"vless-in"}); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("vless-in", "live") {
		t.Fatal("unknown desired users wiped xray")
	}
}

func TestNoDesiredStateDoesNotWipeXray(t *testing.T) {
	fx := newFakeXray()
	fx.users["main/live"] = true
	e := deadEngine(t, fx, filepath.Join(t.TempDir(), "missing.json"))
	if err := e.LoadState(e.cfg.CachePath); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(context.Background(), mainIn); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("main", "live") {
		t.Fatal("unknown desired state wiped live xray users")
	}
}

func TestXrayRestartReappliesUsers(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	e.setDesired(users("a", "b"))
	if err := e.Reconcile(ctx, mainIn); err != nil {
		t.Fatal(err)
	}
	fx.wipe()
	if err := e.Reconcile(ctx, mainIn); err != nil {
		t.Fatal(err)
	}
	if fx.userCount() != 2 {
		t.Fatalf("users after xray restart = %d, want 2", fx.userCount())
	}
}

func TestLoadStateRestoresUsersAndInboundsWithoutTouchingXray(t *testing.T) {
	fx := newFakeXray()
	cachePath := filepath.Join(t.TempDir(), "c.json")
	if err := SaveCache(cachePath, &Cache{Version: 9, Users: users("cached-user"), Inbounds: mainIn}); err != nil {
		t.Fatal(err)
	}
	e := deadEngine(t, fx, cachePath)
	if err := e.LoadState(cachePath); err != nil {
		t.Fatal(err)
	}
	if fx.userCount() != 0 {
		t.Fatal("loading the cache must not touch xray by itself")
	}
	if !reflect.DeepEqual(e.Inbounds(), mainIn) || e.version != 9 {
		t.Fatalf("inbounds = %+v, version = %d", e.Inbounds(), e.version)
	}
	if err := e.Reconcile(context.Background(), e.Inbounds()); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("main", "cached-user") {
		t.Fatal("cached user not applied")
	}
}

func TestLoadStateOldCacheForcesFullSync(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(cachePath, []byte(`{"version":5,"users":[{"uuid":"u-1"}],"config":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fc := &fakeCore{syncVersion: 5, syncUsers: users("u-1"), syncInbounds: mainIn}
	e := testEngine(t, newFakeXray(), fc)
	if err := e.LoadState(cachePath); err != nil {
		t.Fatal(err)
	}
	if e.Inbounds() != nil {
		t.Fatalf("old cache has no inbounds, got %+v", e.Inbounds())
	}
	resp, err := e.SyncOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Users == nil || !reflect.DeepEqual(e.Inbounds(), mainIn) {
		t.Fatalf("agent with an old cache stayed on no-change sync: resp %+v, inbounds %+v", resp, e.Inbounds())
	}
}

func TestLateXrayWithCoreDownServesCachedUsers(t *testing.T) {
	fx := newFakeXray()
	cachePath := filepath.Join(t.TempDir(), "c.json")
	if err := SaveCache(cachePath, &Cache{Version: 4, Users: users("cached"), Inbounds: mainIn}); err != nil {
		t.Fatal(err)
	}
	fx.listErr = errors.New("connection refused")
	e := deadEngine(t, fx, cachePath)
	if err := e.LoadState(cachePath); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(context.Background(), e.Inbounds()); err == nil {
		t.Fatal("want error while xray is not ready")
	}
	fx.set(func(f *fakeXray) { f.listErr = nil })
	if err := e.Reconcile(context.Background(), e.Inbounds()); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("main", "cached") {
		t.Fatal("cached user never applied after xray came up")
	}
}

func TestAgentRestartRemovesRevokedLeftover(t *testing.T) {
	fx := newFakeXray()
	cachePath := filepath.Join(t.TempDir(), "c.json")
	fc := &fakeCore{syncVersion: 2, syncUsers: users("a", "c", "d"), syncInbounds: mainIn}
	fx.failAdd = map[string]bool{"d": true}
	first := testEngine(t, fx, fc)
	first.cfg.CachePath = cachePath
	ctx := context.Background()
	if _, err := first.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.Reconcile(ctx, first.Inbounds()); err == nil {
		t.Fatal("want error from d")
	}
	if err := SaveCache(cachePath, &Cache{Version: 3, Users: users("a"), Inbounds: mainIn}); err != nil {
		t.Fatal(err)
	}
	fx.set(func(f *fakeXray) { f.failAdd = nil })
	second := deadEngine(t, fx, cachePath)
	if err := second.LoadState(cachePath); err != nil {
		t.Fatal(err)
	}
	if err := second.Reconcile(ctx, second.Inbounds()); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("main", "c") {
		t.Fatal("revoked c kept access across agent restart")
	}
}

func TestRemoveFailureRetriedNextTick(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	e.setDesired(users("x"))
	if err := e.Reconcile(ctx, mainIn); err != nil {
		t.Fatal(err)
	}
	fx.set(func(f *fakeXray) { f.failRemove = map[string]bool{"x": true} })
	e.setDesired(nil)
	if err := e.Reconcile(ctx, mainIn); err == nil {
		t.Fatal("want error from failed remove")
	}
	fx.set(func(f *fakeXray) { f.failRemove = nil })
	if err := e.Reconcile(ctx, mainIn); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("main", "x") {
		t.Fatal("revoked x still in xray after retry")
	}
}

func TestFreshAgentGetsListAtVersionZero(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 0, syncUsers: users("u"), syncInbounds: mainIn}
	e := testEngine(t, fx, fc)
	ctx := context.Background()
	if _, err := e.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(ctx, e.Inbounds()); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("main", "u") {
		t.Fatal("fresh agent got no users from node at version 0")
	}
}

func TestSyncSavesCacheEvenIfApplyFails(t *testing.T) {
	fx := newFakeXray()
	fx.failAdd = map[string]bool{"bad": true}
	fc := &fakeCore{syncVersion: 7, syncUsers: users("ok", "bad"), syncInbounds: mainIn}
	e := testEngine(t, fx, fc)
	ctx := context.Background()
	if _, err := e.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(ctx, e.Inbounds()); err == nil {
		t.Fatal("want error from bad")
	}
	cached, err := LoadCache(e.cfg.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Version != 7 || len(cached.Users) != 2 || !reflect.DeepEqual(cached.Inbounds, mainIn) {
		t.Fatalf("cache not updated to last valid state: %+v", cached)
	}
}

func TestSyncOnceNoChangeKeepsDesired(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 3, syncUsers: users("x"), syncInbounds: mainIn}
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
	if !e.ActiveUsers()["x"] || !reflect.DeepEqual(e.Inbounds(), mainIn) {
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
	cfg := &Config{CoreURL: ts.URL, CachePath: filepath.Join(t.TempDir(), "c.json")}
	e := NewEngine(cfg, newCoreClientForTest(cfg, discardLogger()), fx, slog.New(slog.NewJSONHandler(&buf, nil)))
	e.setDesired(users(added, failing))
	err := e.Reconcile(context.Background(), mainIn)
	if err == nil {
		t.Fatal("want error")
	}
	logs := buf.String()
	for _, secret := range []string{added, failing, testPrivateKey} {
		if strings.Contains(logs, secret) || strings.Contains(err.Error(), secret) {
			t.Fatalf("secret leaked: logs=%s err=%v", logs, err)
		}
	}
	if !strings.Contains(logs, "xray_user_added") || !strings.Contains(logs, keyRef(added)) || !strings.Contains(logs, `"inbound":"main"`) {
		t.Fatalf("applied diff not logged: %s", logs)
	}
	if !strings.Contains(logs, keyRef(failing)) {
		t.Fatalf("failure lacks key ref: %s", logs)
	}
}
