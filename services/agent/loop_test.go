package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
)

func testLoop(t *testing.T, fx *fakeXray, coreURL string) (*Loop, *Engine) {
	t.Helper()
	rig := newNodeRig(t, fx)
	cfg := rig.cfg
	cfg.CoreURL = coreURL
	cfg.CachePath = filepath.Join(t.TempDir(), "c.json")
	cfg.SyncInterval = 10 * time.Millisecond
	core := newCoreClientForTest(cfg, discardLogger())
	e := NewEngine(cfg, core, fx, discardLogger())
	sc := NewStatsCollector(fx, discardLogger())
	return NewLoop(e, sc, core, fx, rig.node, cfg, discardLogger()), e
}

func preapply(t *testing.T, loop *Loop, inbounds []Inbound) {
	t.Helper()
	data, err := BuildXray(inbounds, loop.cfg.XrayAPI)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(loop.cfg.XrayConfig, data, xrayFileMode); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(loop.cfg.XrayConfig, old, old); err != nil {
		t.Fatal(err)
	}
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
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, syncInbounds: mainIn}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	if err := loop.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("main", "u-1") {
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
	if err := SaveCache(e.cfg.CachePath, &Cache{Version: 9, Users: []SyncUser{{UUID: "cached-user"}}, Inbounds: mainIn}); err != nil {
		t.Fatal(err)
	}
	if err := e.LoadState(e.cfg.CachePath); err != nil {
		t.Fatal(err)
	}
	if err := loop.RunOnce(context.Background()); err == nil {
		t.Fatal("tick should fail with core down")
	}
	if !fx.hasUser("main", "cached-user") {
		t.Fatal("cached user not served with core down")
	}
}

func TestApplyCachedServesUsersBeforeFirstSync(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 9}
	loop, e := testLoop(t, fx, httptestOf(fc, t).URL)
	if err := SaveCache(e.cfg.CachePath, &Cache{Version: 9, Users: []SyncUser{{UUID: "cached-user"}}, Inbounds: mainIn}); err != nil {
		t.Fatal(err)
	}
	if err := e.LoadState(e.cfg.CachePath); err != nil {
		t.Fatal(err)
	}
	loop.ApplyCached(context.Background())
	if !fx.hasUser("main", "cached-user") {
		t.Fatal("cached user not applied before the first sync")
	}
	fc.mu.Lock()
	beats := fc.heartbeats
	fc.mu.Unlock()
	if beats != 0 {
		t.Fatalf("ApplyCached talked to core: %d heartbeats", beats)
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
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, syncInbounds: mainIn, failDeltas: true}
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
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, syncInbounds: mainIn, failAfterCommit: true}
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
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, syncInbounds: mainIn, failAfterCommit: true}
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
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}, {UUID: "u-2"}}, syncInbounds: testInbounds("alpha", "beta")}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	preapply(t, loop, testInbounds("alpha", "beta"))
	fx.set(func(f *fakeXray) { f.uptime = 90 * time.Second })
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
		t.Fatalf("active_users = %v, want 2 unique users across both inbounds", hb["active_users"])
	}
	if hb["xray_uptime_sec"] != float64(90) {
		t.Fatalf("xray_uptime_sec = %v, want 90", hb["xray_uptime_sec"])
	}
	if up, _ := hb["agent_uptime_sec"].(float64); up < 120 {
		t.Fatalf("agent_uptime_sec = %v, want >= 120", hb["agent_uptime_sec"])
	}
	if v, ok := hb["config_error"]; !ok || v != nil {
		t.Fatalf("config_error = %v (present %v), want null on a healthy node", v, ok)
	}
}

func TestHeartbeatSentWhenXrayProbesFail(t *testing.T) {
	fx := newFakeXray()
	fx.set(func(f *fakeXray) {
		f.listErr = errors.New("xray api down")
		f.uptimeErr = errors.New("xray api down")
	})
	fc := &fakeCore{syncVersion: 2, syncInbounds: mainIn}
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

func TestHeartbeatActiveUsersUnknownWithoutInbounds(t *testing.T) {
	fx := newFakeXray()
	fx.users["main/live"] = true
	loop, _ := testLoop(t, fx, deadCoreURL(t))
	hb := loop.heartbeat(context.Background())
	if hb.ActiveUsers != nil {
		t.Fatalf("active_users = %d, want null while inbounds are unknown", *hb.ActiveUsers)
	}
	fc := &fakeCore{syncVersion: 1, syncUsers: []SyncUser{}, syncInbounds: []Inbound{}}
	loop, _ = testLoop(t, fx, httptestOf(fc, t).URL)
	tick(t, loop)
	hb = loop.heartbeat(context.Background())
	if hb.ActiveUsers == nil || *hb.ActiveUsers != 0 {
		t.Fatalf("active_users = %v, want 0 on a node without inbounds", hb.ActiveUsers)
	}
}

func TestRejectedConfigKeepsUsersOnRunningInbounds(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 1, syncUsers: users("u-1", "u-2"), syncInbounds: mainIn}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	tick(t, loop)
	if !fx.hasUser("main", "u-1") || !fx.hasUser("main", "u-2") {
		t.Fatal("users not applied")
	}
	setCore(fc, func(f *fakeCore) {
		f.syncVersion = 2
		f.syncUsers = users("u-2")
		f.syncInbounds = brokenInbounds()
	})
	if err := loop.RunOnce(context.Background()); err != nil {
		t.Fatalf("a rejected config must not fail the user sync: %v", err)
	}
	if fx.hasUser("main", "u-1") {
		t.Fatal("revoked user kept access on the inbound xray still runs")
	}
	if !fx.hasUser("main", "u-2") {
		t.Fatal("active user lost after rollback")
	}
	hb, _ := lastHeartbeat(fc)
	if msg, _ := hb["config_error"].(string); !strings.HasPrefix(msg, "xray_config_rejected: ") {
		t.Fatalf("heartbeat config_error = %v", hb["config_error"])
	}
	setCore(fc, func(f *fakeCore) {
		f.syncVersion = 3
		f.syncInbounds = testInbounds("main", "extra")
	})
	tick(t, loop)
	if !fx.hasUser("extra", "u-2") {
		t.Fatal("fixed inbounds not applied")
	}
	hb, _ = lastHeartbeat(fc)
	if v, ok := hb["config_error"]; !ok || v != nil {
		t.Fatalf("config_error = %v after a good apply", v)
	}
}

func TestRevokedUserLosesAccessWhileRunningInboundsAreUnknown(t *testing.T) {
	fx := newFakeXray()
	fx.users["vless-in/revoked"] = true
	fx.users["vless-in/keep"] = true
	bad := testInbound("main")
	bad.Reality.Dest = "127.0.0.1:2019"
	fc := &fakeCore{syncVersion: 1, syncUsers: users("keep"), syncInbounds: []Inbound{bad}}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	if err := writeFileAtomic(loop.cfg.XrayConfig, []byte(`{"inbounds":[{"tag":"vless-in","protocol":"vless"}]}`), xrayFileMode); err != nil {
		t.Fatal(err)
	}
	_ = loop.RunOnce(context.Background())
	if loop.applied != nil {
		t.Fatalf("running inbounds = %+v, want unknown", loop.applied)
	}
	if fx.hasUser("vless-in", "revoked") {
		t.Fatal("revoked user kept access because the agent did not know the running inbounds")
	}
	if !fx.hasUser("vless-in", "keep") {
		t.Fatal("active user removed")
	}
}

func TestLoopRestartsXrayOnlyWhenInboundsChange(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 1, syncUsers: users("u-1"), syncInbounds: mainIn}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	proc := loop.node.proc.(*fakeProc)
	tick(t, loop)
	tick(t, loop)
	setCore(fc, func(f *fakeCore) { f.syncVersion = 2; f.syncUsers = users("u-1", "u-2") })
	tick(t, loop)
	if proc.killCount() != 1 {
		t.Fatalf("xray restarted %d times, want once: user changes go through the API", proc.killCount())
	}
	if !fx.hasUser("main", "u-2") {
		t.Fatal("new user not added without restart")
	}
}

func TestHeartbeatReportsOnline(t *testing.T) {
	fx := newFakeXray()
	now := time.Now().Unix()
	fx.online = map[string]map[string]int64{
		"key-b": {"203.0.113.9": now - 30},
		"key-a": {"2001:db8::1": now - 600, "198.51.100.4": now + 5},
	}
	loop, _ := testLoop(t, fx, deadCoreURL(t))
	hb := loop.heartbeat(context.Background())
	if hb.Online == nil || len(*hb.Online) != 2 {
		t.Fatalf("online = %+v", hb.Online)
	}
	a := (*hb.Online)[0]
	if a.Key != "key-a" || len(a.IPs) != 2 || a.IPs[0].IP != "198.51.100.4" || a.IPs[0].AgeSec != 0 || a.IPs[1].AgeSec < 600 {
		t.Fatalf("key-a = %+v", a)
	}
	data, err := json.Marshal(hb)
	if err != nil || !strings.Contains(string(data), `"online":[{"key":"key-a"`) {
		t.Fatalf("json = %s, %v", data, err)
	}

	fx.online = map[string]map[string]int64{}
	if hb = loop.heartbeat(context.Background()); hb.Online == nil || len(*hb.Online) != 0 {
		t.Fatalf("nobody online must be an empty report, got %+v", hb.Online)
	}
	if data, _ = json.Marshal(hb); !strings.Contains(string(data), `"online":[]`) {
		t.Fatalf("json = %s", data)
	}

	fx.onlineErr = errors.New("xray down")
	if hb = loop.heartbeat(context.Background()); hb.Online != nil {
		t.Fatal("a failed probe must not report an empty snapshot")
	}
	if data, _ = json.Marshal(hb); strings.Contains(string(data), `"online"`) {
		t.Fatalf("json = %s", data)
	}

	fx.onlineErr = nil
	fx.online = map[string]map[string]int64{"key-d": {"[2001:DB8::1]": now - 5, "::ffff:203.0.113.1": now, "garbage": now, "198.51.100.1": now - 100*24*3600}}
	hb = loop.heartbeat(context.Background())
	if len(*hb.Online) != 1 {
		t.Fatalf("online = %+v", *hb.Online)
	}
	got := map[string]int64{}
	for _, ip := range (*hb.Online)[0].IPs {
		got[ip.IP] = ip.AgeSec
	}
	if len(got) != 3 || got["2001:db8::1"] < 5 || got["203.0.113.1"] != 0 || got["198.51.100.1"] != maxOnlineAgeSec {
		t.Fatalf("normalized = %v", got)
	}
	fx.online = map[string]map[string]int64{"key-e": {"garbage": now}}
	if hb = loop.heartbeat(context.Background()); len(*hb.Online) != 0 {
		t.Fatalf("a key without valid IPs reported: %+v", *hb.Online)
	}
	many := map[string]int64{}
	for i := range 40 {
		many[fmt.Sprintf("192.0.2.%d", i)] = now
	}
	fx.online = map[string]map[string]int64{"key-c": many}
	if hb = loop.heartbeat(context.Background()); len((*hb.Online)[0].IPs) != maxOnlineIPsPerKey {
		t.Fatalf("ips per key not capped: %d", len((*hb.Online)[0].IPs))
	}
}
