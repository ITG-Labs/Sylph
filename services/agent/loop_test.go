package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
)

func testLoop(t *testing.T, fx *fakeXray, coreURL string) (*Loop, *Engine) {
	t.Helper()
	cfg := &Config{CoreURL: coreURL, InboundTag: "vless-in", CachePath: filepath.Join(t.TempDir(), "c.json"), SyncInterval: 10 * time.Millisecond}
	core := newCoreClientForTest(cfg, discardLogger())
	e := NewEngine(cfg, core, fx, discardLogger())
	sc := NewStatsCollector(fx, discardLogger())
	return NewLoop(e, sc, core, fx, cfg, discardLogger()), e
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

func countedUp(fc *fakeCore) int64 {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	var total int64
	for _, d := range fc.deltasSeen {
		total += d.BytesUp
	}
	return total
}

func setCore(fc *fakeCore, f func(*fakeCore)) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	f(fc)
}

func tick(t *testing.T, loop *Loop) {
	t.Helper()
	if err := loop.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFailedReportResendsSameBatch(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 42})
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, failDeltas: true}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	tick(t, loop)
	setCore(fc, func(f *fakeCore) { f.failDeltas = false })
	fx.setStats(map[string]int64{uplink("u-1"): 100})
	tick(t, loop)
	tick(t, loop)
	fc.mu.Lock()
	attempts := append([]string(nil), fc.batchAttempts...)
	fc.mu.Unlock()
	if len(attempts) != 3 || attempts[0] != attempts[1] || attempts[1] == attempts[2] {
		t.Fatalf("batch ids = %v, want retry of the same id then a new one", attempts)
	}
	if got := countedUp(fc); got != 100 {
		t.Fatalf("counted %d bytes, want 100", got)
	}
}

func TestLostResponseNotDoubleCounted(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 42})
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, failAfterCommit: true}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	tick(t, loop)
	setCore(fc, func(f *fakeCore) { f.failAfterCommit = false })
	fx.setStats(map[string]int64{uplink("u-1"): 100})
	tick(t, loop)
	tick(t, loop)
	if got := countedUp(fc); got != 100 {
		t.Fatalf("counted %d bytes, want 100 (core committed, response lost)", got)
	}
}

func TestCrashAfterSendResendsSameBatch(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 42})
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, failAfterCommit: true}
	url := httptestOf(fc, t).URL
	statsPath := filepath.Join(t.TempDir(), "stats.json")
	first, _ := testLoop(t, fx, url)
	if err := first.stats.Load(statsPath); err != nil {
		t.Fatal(err)
	}
	tick(t, first)
	setCore(fc, func(f *fakeCore) { f.failAfterCommit = false })
	restarted, _ := testLoop(t, fx, url)
	if err := restarted.stats.Load(statsPath); err != nil {
		t.Fatal(err)
	}
	tick(t, restarted)
	fc.mu.Lock()
	attempts := append([]string(nil), fc.batchAttempts...)
	fc.mu.Unlock()
	if len(attempts) != 2 || attempts[0] != attempts[1] {
		t.Fatalf("batch ids = %v, want the same id after restart", attempts)
	}
	if got := countedUp(fc); got != 42 {
		t.Fatalf("counted %d bytes, want 42", got)
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

func lastHeartbeat(fc *fakeCore) (map[string]any, int) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.lastHeartbeat, fc.heartbeats
}

func TestHeartbeatReportsXrayState(t *testing.T) {
	fx := newFakeXray()
	fx.set(func(f *fakeXray) { f.uptime = 90 * time.Second })
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}, {UUID: "u-2"}}}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	loop.cfg.XrayVersion = "25.9.11"
	loop.started = time.Now().Add(-2 * time.Minute)
	tick(t, loop)
	hb, beats := lastHeartbeat(fc)
	if beats != 1 {
		t.Fatalf("heartbeats = %d", beats)
	}
	if hb["agent_version"] != version.Version || hb["xray_version"] != "25.9.11" {
		t.Fatalf("versions = %v", hb)
	}
	if hb["active_users"] != float64(2) {
		t.Fatalf("active_users = %v, want 2 users live in xray", hb["active_users"])
	}
	if hb["xray_uptime_sec"] != float64(90) {
		t.Fatalf("xray_uptime_sec = %v, want 90", hb["xray_uptime_sec"])
	}
	if up, _ := hb["agent_uptime_sec"].(float64); up < 120 {
		t.Fatalf("agent_uptime_sec = %v, want >= 120", hb["agent_uptime_sec"])
	}
}

func TestHeartbeatSentWhenXrayProbesFail(t *testing.T) {
	fx := newFakeXray()
	fx.set(func(f *fakeXray) {
		f.listErr = errors.New("xray api down")
		f.uptimeErr = errors.New("xray api down")
	})
	fc := &fakeCore{syncVersion: 2}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	_ = loop.RunOnce(context.Background())
	hb, beats := lastHeartbeat(fc)
	if beats != 1 {
		t.Fatalf("heartbeats = %d, want heartbeat even with xray down", beats)
	}
	if v, ok := hb["active_users"]; !ok || v != nil {
		t.Fatalf("active_users = %v (present %v), want null", v, ok)
	}
	if v, ok := hb["xray_uptime_sec"]; !ok || v != nil {
		t.Fatalf("xray_uptime_sec = %v (present %v), want null", v, ok)
	}
}
