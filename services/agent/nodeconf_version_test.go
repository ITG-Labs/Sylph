package agent

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestApplyRefusesXrayBelowTheMinimumVersion(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.cfg.XrayVersion = "26.3.27"
	before := r.disk()
	r.node.ApplyDesired(context.Background(), NodeDesired{Inbounds: mainIn})
	e := r.node.ConfigError()
	if e == nil || !strings.Contains(*e, "xray_version_unsupported") || !strings.Contains(*e, "26.3.27") {
		t.Fatalf("config_error = %v", e)
	}
	if r.proc.killCount() != 0 || !bytes.Equal(r.disk(), before) {
		t.Fatalf("kills = %d, the config on disk changed", r.proc.killCount())
	}
}

func TestApplyAcceptsSupportedXrayVersions(t *testing.T) {
	for _, v := range []string{"", "26.10.10", "26.10.21"} {
		r := newNodeRig(t, newFakeXray())
		r.cfg.XrayVersion = v
		r.node.ApplyDesired(context.Background(), NodeDesired{Inbounds: mainIn})
		if e := r.node.ConfigError(); e != nil || r.proc.killCount() != 1 {
			t.Fatalf("version %q: config_error = %v, kills = %d", v, e, r.proc.killCount())
		}
	}
}

func TestApplyBelowTheMinimumVersionStillRestoresCaddy(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.cfg.XrayVersion = "26.10.21"
	r.node.ApplyDesired(context.Background(), NodeDesired{Inbounds: mainIn})
	kills := r.proc.killCount()
	r.caddy.mu.Lock()
	r.caddy.current = nil
	r.caddy.mu.Unlock()
	r.cfg.XrayVersion = "26.3.27"
	r.node.ApplyDesired(context.Background(), NodeDesired{Inbounds: mainIn})
	e := r.node.ConfigError()
	if e == nil || !strings.Contains(*e, "xray_version_unsupported") {
		t.Fatalf("config_error = %v", e)
	}
	current, _, _ := r.caddy.state()
	if !sameJSON(current, r.caddyWant(mainIn)) {
		t.Fatalf("a restarted caddy got no routes: %s", current)
	}
	if r.proc.killCount() != kills {
		t.Fatal("xray was restarted although its version is refused")
	}
}
