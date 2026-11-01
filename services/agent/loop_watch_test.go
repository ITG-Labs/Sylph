package agent

import (
	"context"
	"os"
	"testing"
	"time"
)

func waitUntil(t *testing.T, within time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func runWatchedLoop(t *testing.T, fx *fakeXray) (*fakeCore, func()) {
	t.Helper()
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}, {UUID: "u-2"}}, syncInbounds: mainIn}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	loop.cfg.SyncInterval = time.Hour
	loop.cfg.XrayWatch = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()
	waitUntil(t, 2*time.Second, "first apply", func() bool { return fx.hasUser("main", "u-1") && fx.hasUser("main", "u-2") })
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(loop.cfg.XrayConfig, old, old); err != nil {
		t.Fatal(err)
	}
	fx.mu.Lock()
	fx.uptime = time.Hour
	probes := fx.uptimeHits
	fx.mu.Unlock()
	waitUntil(t, 2*time.Second, "watch saw the running xray", func() bool {
		fx.mu.Lock()
		defer fx.mu.Unlock()
		return fx.uptimeHits >= probes+2
	})
	return fc, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("loop did not stop")
		}
	}
}

func crashXray(fx *fakeXray, uptime time.Duration, failProbes int) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.users = map[string]bool{}
	fx.uptime = uptime
	fx.uptimeFail = failProbes
}

func TestRunRefillsXrayThatRestartedOnItsOwn(t *testing.T) {
	fx := newFakeXray()
	fc, stop := runWatchedLoop(t, fx)
	defer stop()
	_, ticks := lastHeartbeat(fc)
	crashXray(fx, time.Second, 0)
	waitUntil(t, 2*time.Second, "users back after the uptime dropped", func() bool { return fx.hasUser("main", "u-1") && fx.hasUser("main", "u-2") })
	if _, after := lastHeartbeat(fc); after != ticks {
		t.Fatalf("refill must not wait for the next sync tick: ticks %d → %d", ticks, after)
	}
}

func TestRunRefillsXrayAfterItWasUnreachable(t *testing.T) {
	fx := newFakeXray()
	_, stop := runWatchedLoop(t, fx)
	defer stop()
	crashXray(fx, time.Hour, 5)
	waitUntil(t, 2*time.Second, "users back after xray answered again", func() bool { return fx.hasUser("main", "u-1") && fx.hasUser("main", "u-2") })
}

func TestRunWatchLeavesAHealthyXrayAlone(t *testing.T) {
	fx := newFakeXray()
	_, stop := runWatchedLoop(t, fx)
	defer stop()
	time.Sleep(30 * time.Millisecond)
	fx.mu.Lock()
	lists, probes := fx.listHits, fx.uptimeHits
	fx.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	fx.mu.Lock()
	defer fx.mu.Unlock()
	if fx.listHits != lists || fx.uptimeHits < probes+3 {
		t.Fatalf("healthy xray was reconciled again: lists %d → %d, uptime probes %d → %d", lists, fx.listHits, probes, fx.uptimeHits)
	}
}
