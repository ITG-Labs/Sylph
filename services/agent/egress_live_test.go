package agent

import (
	"context"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

func TestLiveEgressRoutesLikeThePlanButLeavesThroughTheAddress(t *testing.T) {
	img := xrayImage(t)
	ctx := context.Background()
	p := &noderoute.Profile{
		Outbounds: []noderoute.Outbound{
			{Tag: "direct", Kind: noderoute.KindDirect, Direct: &noderoute.Direct{IP: "prefer_ipv4"}},
			{Tag: "block", Kind: noderoute.KindBlock},
			{Tag: "nl", Kind: noderoute.KindProxy, Proxy: &noderoute.Proxy{Protocol: "socks", Address: "nl.example.com", Port: 1080}},
			{Tag: "fi", Kind: noderoute.KindProxy, Proxy: &noderoute.Proxy{Protocol: "socks", Address: "fi.example.com", Port: 1080}},
		},
		Rules: []noderoute.Rule{
			{Protocol: []string{"bittorrent"}, To: "block"},
			{Domain: []string{"domain:blocked.example"}, To: "block"},
			{Domain: []string{"domain:viafi.example"}, To: "fi"},
			{Plans: []int64{3}, To: "nl"},
		},
		Default: "direct",
	}
	a, b := "6f1c2a3b-4d5e-4f60-8a7b-00000000000a", "6f1c2a3b-4d5e-4f60-8a7b-00000000000b"
	c, d := "6f1c2a3b-4d5e-4f60-8a7b-00000000000c", "6f1c2a3b-4d5e-4f60-8a7b-00000000000d"
	egress := []noderoute.Egress{{Name: "203-0-113-7", SendThrough: "172.31.226.10", Users: map[int64][]string{3: {a}, 5: {c}}}}
	config, err := BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "0.0.0.0:10085", Routing: p, GeoPrefix: "sylph", Egress: egress})
	if err != nil {
		t.Fatal(err)
	}
	x := runLiveXray(t, img, config)
	k := NewPlanRules(x, discardLogger())
	k.Keep(ctx, p, map[int64][]string{3: {a, b}, 5: {c, d}})
	if k.Error() != nil {
		t.Fatalf("plan rules: %v", *k.Error())
	}
	own := noderoute.EgressTag("203-0-113-7", "direct")
	for _, tc := range []struct {
		domain string
		want   map[string]string
		why    string
	}{
		{"example.com", map[string]string{a: "nl", b: "nl", c: own, d: ""}, "plan rule wins for plan 3; for plan 5 the address replaces the default outbound, which xray picks when no rule matches"},
		{"www.blocked.example", map[string]string{a: "block", b: "block", c: "block", d: "block"}, "blocks apply to everybody"},
		{"viafi.example", map[string]string{a: "fi", b: "fi", c: "fi", d: "fi"}, "proxies are not replaced"},
	} {
		for user, want := range tc.want {
			got, err := x.TestRouteTo(ctx, "alpha", user, tc.domain)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s for %s = %q, want %q (%s)", tc.domain, user[len(user)-1:], got, want, tc.why)
			}
		}
	}
}

func TestLiveXrayAcceptsEgressWithoutProfile(t *testing.T) {
	img := xrayImage(t)
	config, err := BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "0.0.0.0:10085",
		Egress: []noderoute.Egress{{Name: "203-0-113-7", SendThrough: "172.31.226.10", Users: map[int64][]string{3: {"6f1c2a3b-4d5e-4f60-8a7b-00000000000a"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	runLiveXray(t, img, config)
}
