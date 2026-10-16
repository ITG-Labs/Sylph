package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func geoSyncExtra(url string) map[string]any {
	return map[string]any{
		"routing": map[string]any{
			"outbounds": []any{map[string]any{"tag": "direct", "kind": "direct", "direct": map[string]any{"ip": "auto"}}, map[string]any{"tag": "block", "kind": "block"}},
			"rules":     []any{map[string]any{"domain": []string{"ru:ru"}, "to": "block"}},
			"default":   "direct",
		},
		"geo": map[string]any{"generation": 1, "sources": []any{map[string]any{"name": "ru", "kind": "domain", "url": url}}},
	}
}

func TestLoopDownloadsTheGeoBaseThenAppliesRouting(t *testing.T) {
	site := siteDat(t, "example.ru")
	srv := newGeoServer(t, map[string][]byte{"ru.dat": site})
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, syncInbounds: mainIn, syncExtra: geoSyncExtra(srv.URL + "/ru.dat")}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	loop.node.geo = testGeo(t, srv)
	loop.cfg.XrayGeoPrefix = "sylph"
	deadline := time.Now().Add(5 * time.Second)
	for !loop.node.Settled() {
		if time.Now().After(deadline) {
			t.Fatalf("routing never applied: %v", loop.node.ConfigError())
		}
		_ = loop.RunOnce(context.Background())
		time.Sleep(10 * time.Millisecond)
	}
	disk, err := os.ReadFile(loop.cfg.XrayConfig)
	if err != nil || !strings.Contains(string(disk), "ext:sylph/ru.dat:ru") {
		t.Fatalf("config on disk does not use the geo base: %v", err)
	}
	if !fx.hasUser("main", "u-1") {
		t.Fatal("user not served with routing on")
	}
	hb, _ := lastHeartbeat(fc)
	st, _ := hb["geo_status"].(map[string]any)
	ru, _ := st["ru"].(map[string]any)
	if ru["sha256"] != sumOf(site) || hb["config_error"] != nil {
		t.Fatalf("heartbeat = %v", hb)
	}
}
