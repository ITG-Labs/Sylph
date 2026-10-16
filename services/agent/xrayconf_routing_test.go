package agent

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

func routedProfile() *noderoute.Profile {
	return &noderoute.Profile{
		Outbounds: []noderoute.Outbound{
			{Tag: "direct", Kind: noderoute.KindDirect, Direct: &noderoute.Direct{IP: "prefer_ipv4"}},
			{Tag: "block", Kind: noderoute.KindBlock},
			{Tag: "nl", Kind: noderoute.KindProxy, Proxy: &noderoute.Proxy{Protocol: "vless", Address: "nl.example.com", Port: 443,
				UUID: "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b", Security: "reality", Flow: "xtls-rprx-vision", SNI: "www.microsoft.com",
				PublicKey: "Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw", ShortID: "ab12"}},
			{Tag: "fi", Kind: noderoute.KindProxy, Proxy: &noderoute.Proxy{Protocol: "socks", Address: "fi.example.com", Port: 1080}},
		},
		Balancers: []noderoute.Balancer{{Tag: "best", Strategy: noderoute.StrategyLeastPing, Outbounds: []string{"nl", "fi"}, Fallback: "direct"}},
		Rules: []noderoute.Rule{
			{Name: "Торренты", Protocol: []string{"bittorrent"}, To: "block"},
			{Domain: []string{"ru-blocked:ru"}, To: "best"},
			{Plans: []int64{3}, To: "nl"},
		},
		Default: "direct",
	}
}

func TestBuildXrayWithRoutingMatchesGolden(t *testing.T) {
	got, err := BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "127.0.0.1:10085", Routing: routedProfile(),
		ServiceKeys: []string{"svc-key"}, GeoPrefix: "sylph"})
	if err != nil {
		t.Fatal(err)
	}
	if *updateGolden {
		writeGolden(t, "xray_routed.json", got)
	}
	assertGolden(t, "xray_routed.json", got)
	var cfg map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "sylph-p") || strings.Contains(string(got), `"plans"`) {
		t.Fatal("plan rules leaked into the file")
	}
}

func TestBuildXrayWithoutRoutingIsUnchanged(t *testing.T) {
	plain, err := BuildXray(goldenInbounds(), "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	routed, err := BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "127.0.0.1:10085", ServiceKeys: []string{"svc"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(routed) {
		t.Fatal("a node without a profile must keep the old config byte for byte")
	}
}

func TestBuildXrayRejectsUnresolvedRouting(t *testing.T) {
	p := routedProfile()
	p.Outbounds = append(p.Outbounds, noderoute.Outbound{Tag: "c", Kind: noderoute.KindCascade, Cascade: &noderoute.Cascade{NodeID: 1}})
	if _, err := BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "127.0.0.1:10085", Routing: p}); err == nil {
		t.Fatal("unresolved cascade accepted")
	}
	p = routedProfile()
	p.Outbounds[3].Proxy.Address = "127.0.0.1"
	if _, err := BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "127.0.0.1:10085", Routing: p}); err == nil {
		t.Fatal("proxy to loopback accepted")
	}
}

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func writeGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join("testdata", name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingServiceOnlyForPlanRules(t *testing.T) {
	p := routedProfile()
	p.Rules = p.Rules[:2]
	got, err := BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "127.0.0.1:10085", Routing: p, GeoPrefix: "sylph"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "RoutingService") {
		t.Fatal("routing API is open without rules by plan")
	}
	got, err = BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "127.0.0.1:10085", Routing: routedProfile(), GeoPrefix: "sylph"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "RoutingService") {
		t.Fatal("routing API is closed although the profile has rules by plan")
	}
}
