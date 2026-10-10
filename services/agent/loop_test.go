package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func testLoop(t *testing.T, fx *fakeXray, coreURL string) (*Loop, *Engine) {
	t.Helper()
	cfg := &Config{CoreURL: coreURL, InboundTag: "vless-in", CachePath: filepath.Join(t.TempDir(), "c.json"), SyncInterval: 10 * time.Millisecond}
	core := newCoreClientForTest(cfg, discardLogger())
	e := NewEngine(cfg, core, fx, discardLogger())
	sc := NewStatsCollector(fx, discardLogger())
	return NewLoop(e, sc, core, cfg, discardLogger()), e
}

func deadCoreURL(t *testing.T) string {
	t.Helper()
	ts := httptestOf(&fakeCore{}, t)
	url := ts.URL
	ts.Close()
	return url
}

func TestRunOnceFullCycle(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 42})
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	if err := loop.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("vless-in", "u-1") {
		t.Fatal("user not applied")
	}
	if len(fc.deltasSeen) != 1 || fc.deltasSeen[0].BytesUp != 42 {
		t.Fatalf("deltas = %+v", fc.deltasSeen)
	}
	if fc.heartbeats != 1 {
		t.Fatalf("heartbeats = %d", fc.heartbeats)
	}
}

func TestRunOnceCoreDownKeepsXray(t *testing.T) {
	fx := newFakeXray()
	loop, _ := testLoop(t, fx, deadCoreURL(t))
	if err := loop.RunOnce(context.Background()); err == nil {
		t.Fatal("core down should fail")
	}
	if fx.userCount() != 0 {
		t.Fatalf("xray mutated: %d", fx.userCount())
	}
}

func TestLoopStartsWithCoreDown(t *testing.T) {
	fx := newFakeXray()
	loop, e := testLoop(t, fx, deadCoreURL(t))
	if err := SaveCache(e.cfg.CachePath, &Cache{Version: 9, Users: []SyncUser{{UUID: "cached-user"}}}); err != nil {
		t.Fatal(err)
	}
	if err := e.LoadState(e.cfg.CachePath); err != nil {
		t.Fatal(err)
	}
	if err := loop.RunOnce(context.Background()); err == nil {
		t.Fatal("tick should fail with core down")
	}
	if !fx.hasUser("vless-in", "cached-user") {
		t.Fatal("cached user lost after failed tick")
	}
}

func TestFailedReportKeepsTrafficForNextTick(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 42})
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, failDeltas: true}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	if err := loop.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc.mu.Lock()
	fc.failDeltas = false
	fc.mu.Unlock()
	fx.setStats(map[string]int64{uplink("u-1"): 100})
	if err := loop.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.deltasSeen) != 1 || fc.deltasSeen[0].BytesUp != 100 {
		t.Fatalf("traffic lost across failed report: %+v", fc.deltasSeen)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 1}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not stop on cancel")
	}
	fc.mu.Lock()
	beats := fc.heartbeats
	fc.mu.Unlock()
	if beats < 2 {
		t.Fatalf("loop ticked %d times, want several", beats)
	}
}
