package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func testEngine(t *testing.T, fx *fakeXray, fc *fakeCore) *Engine {
	t.Helper()
	ts := httptestOf(fc, t)
	cfg := &Config{CoreURL: ts.URL, InboundTag: "vless-in", CachePath: filepath.Join(t.TempDir(), "c.json")}
	core := newCoreClientForTest(cfg, discardLogger())
	return NewEngine(cfg, core, fx, discardLogger())
}

func TestApplyDiff(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	if err := e.applyDiff(context.Background(), []SyncUser{{UUID: "a"}, {UUID: "b"}}); err != nil {
		t.Fatal(err)
	}
	if fx.userCount() != 2 {
		t.Fatalf("users = %d", fx.userCount())
	}
}

func TestApplyDiffRemovesMissing(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	if err := e.applyDiff(ctx, []SyncUser{{UUID: "a"}, {UUID: "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := e.applyDiff(ctx, []SyncUser{{UUID: "b"}, {UUID: "c"}}); err != nil {
		t.Fatal(err)
	}
	if fx.userCount() != 2 {
		t.Fatalf("users = %d", fx.userCount())
	}
	if fx.hasUser("vless-in", "a") {
		t.Fatal("user a should be removed")
	}
	if !fx.hasUser("vless-in", "c") {
		t.Fatal("user c should be added")
	}
}

func TestApplyNoChange(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	ctx := context.Background()
	if err := e.applyDiff(ctx, []SyncUser{{UUID: "a"}}); err != nil {
		t.Fatal(err)
	}
	fx.addErr = errors.New("must not be called")
	if err := e.applyDiff(ctx, []SyncUser{{UUID: "a"}}); err != nil {
		t.Fatalf("unchanged set touched xray: %v", err)
	}
}

func TestApplyDiffBadConfigNoCrash(t *testing.T) {
	fx := newFakeXray()
	fx.addErr = errors.New("xray down")
	e := testEngine(t, fx, &fakeCore{})
	if err := e.applyDiff(context.Background(), []SyncUser{{UUID: "a"}}); err == nil {
		t.Fatal("want error")
	}
	if len(e.ActiveUsers()) != 0 {
		t.Fatalf("failed apply must not record users: %v", e.ActiveUsers())
	}
}

func TestSyncOnce(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 3, syncUsers: []SyncUser{{UUID: "x"}}}
	e := testEngine(t, fx, fc)
	resp, err := e.SyncOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Version != 3 {
		t.Fatalf("version = %d", resp.Version)
	}
	if !fx.hasUser("vless-in", "x") {
		t.Fatal("user x not applied")
	}
	if e.CurrentVersion() != 3 {
		t.Fatalf("engine version = %d", e.CurrentVersion())
	}
	cached, err := LoadCache(e.cfg.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Version != 3 || len(cached.Users) != 1 {
		t.Fatalf("cache = %+v", cached)
	}
	resp2, err := e.SyncOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Users != nil {
		t.Fatalf("no-change sync returned users: %+v", resp2.Users)
	}
}

func TestSyncOnceCoreDown(t *testing.T) {
	fx := newFakeXray()
	ts := httptestOf(&fakeCore{}, t)
	url := ts.URL
	ts.Close()
	cfg := &Config{CoreURL: url, InboundTag: "vless-in", CachePath: filepath.Join(t.TempDir(), "c.json")}
	e := NewEngine(cfg, newCoreClientForTest(cfg, discardLogger()), fx, discardLogger())
	if _, err := e.SyncOnce(context.Background()); err == nil {
		t.Fatal("core down should error")
	}
	if fx.userCount() != 0 {
		t.Fatalf("xray should be untouched: %d", fx.userCount())
	}
}

func TestLoadStateAppliesCache(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	if err := SaveCache(e.cfg.CachePath, &Cache{Version: 9, Users: []SyncUser{{UUID: "cached-user"}}}); err != nil {
		t.Fatal(err)
	}
	if err := e.LoadState(e.cfg.CachePath); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("vless-in", "cached-user") {
		t.Fatal("cached user not applied")
	}
	if e.CurrentVersion() != 9 {
		t.Fatalf("version = %d", e.CurrentVersion())
	}
}
