package agent

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

type guardedConfig struct {
	Outbounds []map[string]any `json:"outbounds"`
	Routing   struct {
		DomainStrategy string           `json:"domainStrategy"`
		Rules          []map[string]any `json:"rules"`
	} `json:"routing"`
}

func decodeGuarded(t *testing.T, data []byte) guardedConfig {
	t.Helper()
	var cfg guardedConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func requireGuard(t *testing.T, cfg guardedConfig) {
	t.Helper()
	if len(cfg.Outbounds) == 0 || cfg.Outbounds[0]["tag"] != "direct" {
		t.Fatalf("the default outbound must stay first: %v", cfg.Outbounds)
	}
	if !slices.ContainsFunc(cfg.Outbounds, func(o map[string]any) bool {
		return o["tag"] == noderoute.GuardOutboundTag && o["protocol"] == "blackhole"
	}) {
		t.Fatalf("no guard outbound: %v", cfg.Outbounds)
	}
	if len(cfg.Routing.Rules) < 2 {
		t.Fatalf("rules = %v", cfg.Routing.Rules)
	}
	ips, names := cfg.Routing.Rules[0], cfg.Routing.Rules[1]
	if ips["ruleTag"] != noderoute.GuardRuleTag || ips["outboundTag"] != noderoute.GuardOutboundTag || !slices.Contains(toStrings(ips["ip"]), "127.0.0.0/8") {
		t.Fatalf("first rule = %v", ips)
	}
	if names["ruleTag"] != noderoute.GuardNamesRuleTag || names["outboundTag"] != noderoute.GuardOutboundTag || !slices.Contains(toStrings(names["domain"]), "full:localhost") {
		t.Fatalf("second rule = %v", names)
	}
}

func toStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, e := range list {
		s, _ := e.(string)
		out = append(out, s)
	}
	return out
}

func TestBuildXrayGuardsLocalTargetsWithoutProfile(t *testing.T) {
	data, err := BuildXray(testInbounds("main"), "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeGuarded(t, data)
	requireGuard(t, cfg)
	if len(cfg.Routing.Rules) != 2 || cfg.Routing.DomainStrategy != "AsIs" {
		t.Fatalf("routing = %+v", cfg.Routing)
	}
}

func TestBuildXrayGuardsLocalTargetsWithEgressOnly(t *testing.T) {
	data, err := BuildXrayRouted(XrayInput{Inbounds: testInbounds("main"), APIListen: "127.0.0.1:10085",
		Egress: []noderoute.Egress{{Name: "203-0-113-7", SendThrough: "172.31.226.10", Users: map[int64][]string{3: {"k1"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeGuarded(t, data)
	requireGuard(t, cfg)
	if len(cfg.Routing.Rules) != 3 || cfg.Routing.Rules[2]["outboundTag"] != noderoute.EgressTag("203-0-113-7", "direct") {
		t.Fatalf("the egress rule must come after the guard: %v", cfg.Routing.Rules)
	}
}

func TestBuildXrayGuardsLocalTargetsWithProfile(t *testing.T) {
	data, err := BuildXrayRouted(XrayInput{Inbounds: testInbounds("main"), APIListen: "127.0.0.1:10085", Routing: routedProfile(), GeoPrefix: "sylph"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeGuarded(t, data)
	if len(cfg.Routing.Rules) < 2 || cfg.Routing.Rules[0]["ruleTag"] != noderoute.GuardRuleTag || cfg.Routing.Rules[1]["ruleTag"] != noderoute.GuardNamesRuleTag {
		t.Fatalf("rules = %v", cfg.Routing.Rules)
	}
}
