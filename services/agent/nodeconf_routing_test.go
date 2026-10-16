package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

func geoRoutedProfile() *noderoute.Profile {
	p := routedProfile()
	p.Rules = []noderoute.Rule{{Domain: []string{"ru:ru"}, To: "block"}}
	return p
}

func (r *nodeRig) withGeo() *GeoManager {
	r.t.Helper()
	dir := filepath.Dir(r.cfg.NodeStatePath)
	g := NewGeoManager(filepath.Join(dir, "geo"), filepath.Join(dir, "geo.json"), discardLogger())
	r.node.geo = g
	r.cfg.XrayGeoPrefix = "sylph"
	return g
}

func putGeo(t *testing.T, g *GeoManager, name string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(g.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := g.path(name)
	if err := os.WriteFile(path, siteDat(t, name+".example"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	g.state[name] = geoEntry{SHA256: "sum-" + mtime.Format(time.RFC3339Nano)}
}

func TestApplyRoutingWritesTheRoutedConfig(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	putGeo(t, r.withGeo(), "ru-blocked", time.Now().Add(-2*time.Hour))
	d := NodeDesired{Inbounds: mainIn, Routing: routedProfile(), ServiceKeys: []string{"svc"}}
	r.node.ApplyDesired(context.Background(), d)
	want, err := BuildXrayRouted(XrayInput{Inbounds: mainIn, APIListen: r.cfg.XrayAPI, Routing: routedProfile(), ServiceKeys: []string{"svc"}, GeoPrefix: "sylph"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r.disk(), want) || r.proc.killCount() != 1 || !r.node.Settled() || r.node.ConfigError() != nil {
		t.Fatalf("kills %d, settled %v, error %v", r.proc.killCount(), r.node.Settled(), r.node.ConfigError())
	}
	reloaded := r.newNode()
	reloaded.geo = r.node.geo
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if reloaded.state.Applied == nil || reloaded.state.AppliedRouting == nil {
		t.Fatal("a routed config on disk was not recognised after a restart")
	}
}

func TestApplyWaitsForAMissingGeoBase(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	g := r.withGeo()
	d := NodeDesired{Inbounds: mainIn, Routing: geoRoutedProfile()}
	r.node.ApplyDesired(context.Background(), d)
	if r.proc.killCount() != 0 || r.node.Settled() {
		t.Fatal("applied a config whose geo base is missing")
	}
	if e := r.node.ConfigError(); e == nil || !strings.Contains(*e, "geo_missing") || !strings.Contains(*e, "ru") {
		t.Fatalf("config_error = %v", e)
	}
	putGeo(t, g, "ru", time.Now().Add(-2*time.Hour))
	r.node.ApplyDesired(context.Background(), d)
	if r.proc.killCount() != 1 || !r.node.Settled() || r.node.ConfigError() != nil {
		t.Fatalf("kills %d, error %v", r.proc.killCount(), r.node.ConfigError())
	}
}

func TestANewGeoBaseRestartsXray(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	g := r.withGeo()
	putGeo(t, g, "ru", time.Now().Add(-2*time.Hour))
	d := NodeDesired{Inbounds: mainIn, Routing: geoRoutedProfile()}
	r.node.ApplyDesired(context.Background(), d)
	r.age(time.Hour)
	r.fx.set(func(f *fakeXray) { f.uptime = 30 * time.Minute })
	r.node.ApplyDesired(context.Background(), d)
	if r.proc.killCount() != 1 {
		t.Fatalf("restarted without a reason: %d", r.proc.killCount())
	}
	putGeo(t, g, "ru", time.Now())
	r.node.ApplyDesired(context.Background(), d)
	if r.proc.killCount() != 2 || !r.node.Settled() {
		t.Fatalf("a fresh geo base did not restart xray: %d", r.proc.killCount())
	}
}

func TestApplyWithoutRoutingKeepsWorkingWithoutGeo(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.node.ApplyDesired(context.Background(), NodeDesired{Inbounds: mainIn})
	if r.proc.killCount() != 1 || !bytes.Equal(r.disk(), r.built(mainIn)) || !r.node.Settled() {
		t.Fatalf("plain apply: kills %d", r.proc.killCount())
	}
}

func TestARejectedConfigIsRetriedWithANewGeoBase(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	g := r.withGeo()
	putGeo(t, g, "ru", time.Now().Add(-2*time.Hour))
	ctx := context.Background()
	r.node.ApplyDesired(ctx, NodeDesired{Inbounds: mainIn, Routing: geoRoutedProfile()})
	broken := NodeDesired{Inbounds: brokenInbounds(), Routing: geoRoutedProfile()}
	r.node.ApplyDesired(ctx, broken)
	kills := r.proc.killCount()
	r.node.ApplyDesired(ctx, broken)
	if r.proc.killCount() != kills {
		t.Fatal("a rejected config was retried with the same geo base")
	}
	putGeo(t, g, "ru", time.Now())
	r.node.ApplyDesired(ctx, broken)
	if r.proc.killCount() == kills {
		t.Fatal("a rejected config was not retried after the geo base changed")
	}
}

func TestABadGeoBaseIsRolledBackWithTheConfig(t *testing.T) {
	good, bad := siteDat(t, "ru.example"), siteDat(t, "broken.example")
	srv := newGeoServer(t, map[string][]byte{"ru.dat": good})
	r := newNodeRig(t, newFakeXray())
	g := r.withGeo()
	g.client, g.allowHost = srv.Client(), func(string) bool { return true }
	ctx := context.Background()
	geo := &SyncGeo{Generation: 1, Sources: []GeoSource{{Name: "ru", Kind: "domain", URL: srv.URL + "/ru.dat"}}}
	d := NodeDesired{Inbounds: mainIn, Routing: geoRoutedProfile(), Geo: geo}
	g.Ensure(ctx, geo, nil)
	r.node.ApplyDesired(ctx, d)
	if !r.node.Settled() {
		t.Fatalf("first apply: %v", r.node.ConfigError())
	}
	r.age(time.Hour)
	r.fx.set(func(f *fakeXray) { f.uptime = 30 * time.Minute })
	r.proc.broken = func() bool {
		data, _ := os.ReadFile(g.path("ru"))
		return bytes.Contains(data, []byte("broken.example"))
	}
	srv.files["ru.dat"] = bad
	geo.Generation = 2
	g.Ensure(ctx, geo, nil)
	r.node.ApplyDesired(ctx, d)
	data, _ := os.ReadFile(g.path("ru"))
	if !bytes.Equal(data, good) {
		t.Fatal("the bad geo base stayed on disk after the rollback")
	}
	if st := g.Status()["ru"]; st.SHA256 != sumOf(good) || st.Error == "" {
		t.Fatalf("geo status = %+v", st)
	}
	if _, err := r.fx.Uptime(ctx); err != nil {
		t.Fatal("xray is down after the rollback")
	}
	kills := r.proc.killCount()
	r.node.ApplyDesired(ctx, d)
	if r.proc.killCount() != kills || !r.node.Settled() || r.node.ConfigError() != nil {
		t.Fatalf("after the rollback: kills %d, error %v", r.proc.killCount()-kills, r.node.ConfigError())
	}
}
