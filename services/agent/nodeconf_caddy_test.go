package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func (r *nodeRig) caddyWant(in []Inbound) []byte {
	r.t.Helper()
	data, err := BuildCaddy(in, r.cfg.CaddyAdmin)
	if err != nil {
		r.t.Fatal(err)
	}
	return data
}

func TestApplyLoadsCaddyRoutesAfterXray(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.node.Apply(context.Background(), mainIn)
	current, loads, loadedAt := r.caddy.state()
	if loads != 1 || !sameJSON(current, r.caddyWant(mainIn)) {
		t.Fatalf("caddy loads = %d, config = %s", loads, current)
	}
	r.proc.mu.Lock()
	killedAt := r.proc.killedAt
	r.proc.mu.Unlock()
	if !killedAt.Before(loadedAt) {
		t.Fatal("caddy got routes before xray listened on the new ports")
	}
	if r.node.ConfigError() != nil {
		t.Fatalf("config_error = %q", *r.node.ConfigError())
	}
}

func TestCaddySameConfigIsNotReloaded(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	var parsed any
	if err := json.Unmarshal(r.caddyWant(mainIn), &parsed); err != nil {
		t.Fatal(err)
	}
	compact, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	r.caddy.set(func(f *fakeCaddy) { f.current = compact })
	r.node.Apply(context.Background(), mainIn)
	if _, loads, _ := r.caddy.state(); loads != 0 {
		t.Fatal("caddy reloaded although it runs the same config in other formatting")
	}
}

func TestCaddyRoutesRestoredAfterCaddyRestart(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.node.Apply(ctx, mainIn)
	kills := r.proc.killCount()
	r.caddy.set(func(f *fakeCaddy) { f.current = []byte(`{"admin":{"listen":"x"}}`) })
	r.node.Apply(ctx, mainIn)
	current, loads, _ := r.caddy.state()
	if loads != 2 || !sameJSON(current, r.caddyWant(mainIn)) {
		t.Fatalf("lost routes not restored: loads %d", loads)
	}
	if r.proc.killCount() != kills {
		t.Fatal("caddy restart must not restart xray")
	}
}

func TestCaddyRejectionIsRemembered(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.caddy.set(func(f *fakeCaddy) { f.reject = "loading new config: layer4: bad route" })
	r.node.Apply(ctx, mainIn)
	e := r.node.ConfigError()
	if e == nil || *e != "caddy_config_rejected: loading new config: layer4: bad route" {
		t.Fatalf("config_error = %v", e)
	}
	r.caddy.set(func(f *fakeCaddy) { f.reject = "" })
	r.node.Apply(ctx, mainIn)
	if _, loads, _ := r.caddy.state(); loads != 0 {
		t.Fatal("rejected caddy config loaded again")
	}
	restarted := r.newNode()
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	if e := restarted.ConfigError(); e == nil || !strings.HasPrefix(*e, "caddy_config_rejected: ") {
		t.Fatalf("config_error lost across restart: %v", e)
	}
	restarted.Apply(ctx, mainIn)
	if _, loads, _ := r.caddy.state(); loads != 0 {
		t.Fatal("agent restart forgot the caddy rejection")
	}
	restarted.Apply(ctx, testInbounds("main", "second"))
	if _, loads, _ := r.caddy.state(); loads != 1 || restarted.ConfigError() != nil {
		t.Fatalf("new inbounds must be tried: loads %d, err %v", loads, restarted.ConfigError())
	}
}

func TestCaddyFollowsRunningInboundsAfterXrayRejection(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.node.Apply(ctx, mainIn)
	r.node.Apply(ctx, brokenInbounds())
	current, loads, _ := r.caddy.state()
	if loads != 1 || !sameJSON(current, r.caddyWant(mainIn)) {
		t.Fatalf("caddy moved to inbounds xray does not run: loads %d", loads)
	}
}

func TestCaddyUnreachableIsRetried(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.node.caddy = NewCaddyAPI("127.0.0.1:1", 200*time.Millisecond)
	r.node.Apply(ctx, mainIn)
	if e := r.node.ConfigError(); e == nil || !strings.HasPrefix(*e, "caddy_unreachable: ") {
		t.Fatalf("config_error = %v", e)
	}
	if r.node.state.CaddyRejected != nil {
		t.Fatal("unreachable caddy recorded as a rejection")
	}
	r.node.caddy = NewCaddyAPI(r.cfg.CaddyAdmin, 2*time.Second)
	r.caddy.set(func(f *fakeCaddy) { f.dropLoad = true })
	r.node.Apply(ctx, mainIn)
	if e := r.node.ConfigError(); e == nil || !strings.HasPrefix(*e, "caddy_unreachable: ") || r.node.state.CaddyRejected != nil {
		t.Fatalf("connection dropped during load = %v, want a transient error, not a rejection", e)
	}
	r.caddy.set(func(f *fakeCaddy) { f.dropLoad = false })
	r.node.Apply(ctx, mainIn)
	if _, loads, _ := r.caddy.state(); loads != 1 || r.node.ConfigError() != nil {
		t.Fatalf("caddy not loaded once reachable: loads %d", loads)
	}
}

func TestCaddyUntouchedWhileRunningInboundsUnknown(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.fx.set(func(f *fakeXray) { f.uptimeErr = errors.New("xray is down") })
	r.caddy.set(func(f *fakeCaddy) { f.current = []byte(`{"apps":{"layer4":{}}}`) })
	r.node.Apply(context.Background(), mainIn)
	if _, loads, _ := r.caddy.state(); loads != 0 {
		t.Fatal("caddy routes replaced although the agent does not know which inbounds xray runs")
	}
}

func TestConfigErrorJoinsXrayAndCaddy(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.node.Apply(ctx, mainIn)
	r.caddy.set(func(f *fakeCaddy) { f.reject = "nope"; f.current = nil })
	r.node.Apply(ctx, brokenInbounds())
	e := r.node.ConfigError()
	if e == nil || !strings.HasPrefix(*e, "xray_config_rejected: ") || !strings.HasSuffix(*e, "; caddy_config_rejected: nope") {
		t.Fatalf("config_error = %v", e)
	}
}

func TestUnknownInboundsDoNotTouchCaddy(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.node.Apply(context.Background(), nil)
	if _, loads, _ := r.caddy.state(); loads != 0 {
		t.Fatal("unknown inbounds touched caddy")
	}
}
