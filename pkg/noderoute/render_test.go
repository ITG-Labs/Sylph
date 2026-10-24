package noderoute

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"slices"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func resolvedProfile() Profile {
	p := goodProfile()
	p.Outbounds[7] = Outbound{Tag: "to-de", Kind: KindProxy, Proxy: &Proxy{Protocol: "vless", Address: "de1.example.com", Port: 443, UUID: testUUID,
		Security: "reality", Flow: "xtls-rprx-vision", SNI: "www.apple.com", Fingerprint: "chrome", PublicKey: testPublicKey, ShortID: "cd34"}}
	return p
}

func geoFile(source string) string { return "sylph/" + source + ".dat" }

func renderJSON(t *testing.T, s Static) []byte {
	t.Helper()
	data, err := json.MarshalIndent(map[string]any{"outbounds": s.Outbounds, "routing": s.Routing, "observatory": s.Observatory}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestRenderGolden(t *testing.T) {
	p := resolvedProfile()
	s, err := Render(RenderInput{Profile: &p, ServiceKeys: []string{"svc-1"}, GeoFile: geoFile})
	if err != nil {
		t.Fatal(err)
	}
	got := renderJSON(t, s)
	again, err := Render(RenderInput{Profile: &p, ServiceKeys: []string{"svc-1"}, GeoFile: geoFile})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, renderJSON(t, again)) {
		t.Fatal("render is not deterministic")
	}
	const golden = "testdata/render.golden.json"
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("render differs from %s:\n%s", golden, got)
	}
}

func TestRenderRefusesUnresolvedCascade(t *testing.T) {
	p := goodProfile()
	if _, err := Render(RenderInput{Profile: &p}); fieldOf(t, err) != "outbounds[7].kind" {
		t.Fatalf("err = %v", err)
	}
}

func TestRenderDefaultOutboundGoesFirst(t *testing.T) {
	p := resolvedProfile()
	p.Default = "nl"
	s, err := Render(RenderInput{Profile: &p, GeoFile: geoFile})
	if err != nil {
		t.Fatal(err)
	}
	if s.Outbounds[0]["tag"] != "nl" || s.Outbounds[1]["tag"] != "direct" {
		t.Fatalf("order = %v %v", s.Outbounds[0]["tag"], s.Outbounds[1]["tag"])
	}
	for _, o := range s.Outbounds {
		if o["tag"] == ServiceOutboundTag {
			t.Fatal("service outbound without service keys")
		}
	}
	rules := s.Routing["rules"].([]any)
	if last := rules[len(rules)-1].(map[string]any); last["ruleTag"] == DefaultRuleTag {
		t.Fatal("catch-all rule for an outbound default")
	}
}

func TestTail(t *testing.T) {
	p := resolvedProfile()
	if err := ValidateResolved(&p); err != nil {
		t.Fatal(err)
	}
	if got := Tail(&p, map[int64][]string{}, 1); got != nil {
		t.Fatalf("tail without users = %+v", got)
	}
	got := Tail(&p, map[int64][]string{3: {"u2", "u1", "u1"}, 4: {"x"}}, 7)
	if len(got) != 1 || got[0].Tag != "sylph-p7-r4" || got[0].BalancerTag != "best" || got[0].OutboundTag != "" ||
		len(got[0].Users) != 2 || got[0].Users[0] != "u1" || got[0].Users[1] != "u2" {
		t.Fatalf("tail = %+v", got)
	}
	p.Default = "best"
	if got := Tail(&p, map[int64][]string{}, 2); len(got) != 1 || !got[0].CatchAll || got[0].Tag != "sylph-p2-default" {
		t.Fatalf("tail without users loses the balancer default = %+v", got)
	}
	got = Tail(&p, map[int64][]string{3: {"u1"}}, 8)
	if len(got) != 2 || !got[1].CatchAll || got[1].BalancerTag != "best" || got[1].Tag != "sylph-p8-default" {
		t.Fatalf("tail with a balancer default = %+v", got)
	}
	s, err := Render(RenderInput{Profile: &p, GeoFile: geoFile})
	if err != nil {
		t.Fatal(err)
	}
	rules := s.Routing["rules"].([]any)
	if last := rules[len(rules)-1].(map[string]any); last["ruleTag"] != DefaultRuleTag || last["balancerTag"] != "best" {
		t.Fatalf("static catch-all = %v", last)
	}
	if !HasPlanRules(&p) {
		t.Fatal("plan rules not seen")
	}
}

func TestRenderGuardsInternalNetworksFirst(t *testing.T) {
	for _, keys := range [][]string{nil, {"svc"}} {
		p := resolvedProfile()
		s, err := Render(RenderInput{Profile: &p, ServiceKeys: keys, GeoFile: geoFile})
		if err != nil {
			t.Fatal(err)
		}
		first := s.Routing["rules"].([]any)[0].(map[string]any)
		if first["ruleTag"] != GuardRuleTag || first["outboundTag"] != GuardOutboundTag {
			t.Fatalf("first rule = %v", first)
		}
		ips, _ := first["ip"].([]string)
		for _, want := range []string{"127.0.0.0/8", "10.0.0.0/8", "169.254.0.0/16", "100.64.0.0/10", "::1/128", "fc00::/7", "64:ff9b::/96"} {
			if !slices.Contains(ips, want) {
				t.Fatalf("guard misses %s: %v", want, ips)
			}
		}
		names := s.Routing["rules"].([]any)[1].(map[string]any)
		domains, _ := names["domain"].([]string)
		if names["ruleTag"] != GuardNamesRuleTag || names["outboundTag"] != GuardOutboundTag || !slices.Contains(domains, "full:localhost") || !slices.Contains(domains, "domain:internal") {
			t.Fatalf("name guard = %v", names)
		}
		last := s.Outbounds[len(s.Outbounds)-1]
		if last["tag"] != GuardOutboundTag || last["protocol"] != "blackhole" {
			t.Fatalf("guard outbound = %v", last)
		}
	}
	p := resolvedProfile()
	p.Rules = []Rule{{Protocol: []string{"bittorrent"}, To: "block"}}
	s, err := Render(RenderInput{Profile: &p, GeoFile: geoFile})
	if err != nil {
		t.Fatal(err)
	}
	if s.Routing["domainStrategy"] != "AsIs" {
		t.Fatalf("the guard must not switch the domain strategy: %v", s.Routing["domainStrategy"])
	}
}

func egressProfile() Profile {
	return Profile{
		Outbounds: []Outbound{
			{Tag: "direct", Kind: KindDirect, Direct: &Direct{IP: "auto"}},
			{Tag: "v6", Kind: KindDirect, Direct: &Direct{IP: "ipv6"}},
			{Tag: "block", Kind: KindBlock},
			{Tag: "de", Kind: KindProxy, Proxy: &Proxy{Protocol: "socks", Address: "de.example.com", Port: 1080}},
		},
		Balancers: []Balancer{{Tag: "bal", Strategy: StrategyRandom, Outbounds: []string{"direct", "de"}}},
		Rules: []Rule{
			{Domain: []string{"geosite:category-ru"}, To: "block"},
			{IP: []string{"1.1.1.1"}, To: "v6"},
			{Domain: []string{"domain:example.org"}, To: "bal"},
			{Protocol: []string{"bittorrent"}, To: "direct"},
			{Plans: []int64{7}, To: "de"},
		},
		Default: "direct",
	}
}

func ruleTags(t *testing.T, s Static) []string {
	t.Helper()
	var tags []string
	for _, r := range s.Routing["rules"].([]any) {
		tags = append(tags, r.(map[string]any)["ruleTag"].(string))
	}
	return tags
}

func ruleByTag(t *testing.T, s Static, tag string) map[string]any {
	t.Helper()
	for _, r := range s.Routing["rules"].([]any) {
		if m := r.(map[string]any); m["ruleTag"] == tag {
			return m
		}
	}
	t.Fatalf("rule %s not rendered", tag)
	return nil
}

func TestRenderEgress(t *testing.T) {
	p := egressProfile()
	plain, err := Render(RenderInput{Profile: &p, GeoFile: geoFile})
	if err != nil {
		t.Fatal(err)
	}
	e := Egress{Name: "203-0-113-7", SendThrough: "172.31.226.10", Users: map[int64][]string{7: {"u-b", "u-a"}, 3: {"u-c"}}}
	s, err := Render(RenderInput{Profile: &p, ServiceKeys: []string{"svc"}, GeoFile: geoFile, Egress: []Egress{e}})
	if err != nil {
		t.Fatal(err)
	}

	var clones []map[string]any
	for _, o := range s.Outbounds {
		if o["sendThrough"] != nil {
			clones = append(clones, o)
		}
	}
	if len(clones) != 2 || clones[0]["tag"] != "_egress-203-0-113-7-direct" || clones[1]["tag"] != "_egress-203-0-113-7-v6" {
		t.Fatalf("clones = %v", clones)
	}
	for _, c := range clones {
		if c["protocol"] != "freedom" || c["sendThrough"] != "172.31.226.10" || c["settings"].(map[string]any)["targetStrategy"] != "ForceIPv4" {
			t.Fatalf("clone = %v", c)
		}
	}
	if len(s.Outbounds) != len(plain.Outbounds)+3 {
		t.Fatalf("outbounds %d, plain %d: only clones and the service outbound may be added", len(s.Outbounds), len(plain.Outbounds))
	}

	want := []string{GuardRuleTag, GuardNamesRuleTag, ServiceRuleTag,
		"sylph-e203-0-113-7-p3-r1", "sylph-e203-0-113-7-p3-r2", "sylph-e203-0-113-7-p3-r3", "sylph-e203-0-113-7-p3-r4", "sylph-e203-0-113-7-p3-default",
		"sylph-e203-0-113-7-p7-r1", "sylph-e203-0-113-7-p7-r2", "sylph-e203-0-113-7-p7-r3", "sylph-e203-0-113-7-p7-r4", "sylph-e203-0-113-7-p7-r5",
		"sylph-r1", "sylph-r2", "sylph-r3", "sylph-r4"}
	if got := ruleTags(t, s); !slices.Equal(got, want) {
		t.Fatalf("rule order:\n got %v\nwant %v", got, want)
	}
	if got := ruleTags(t, plain); !slices.Equal(got, []string{GuardRuleTag, GuardNamesRuleTag, "sylph-r1", "sylph-r2", "sylph-r3", "sylph-r4"}) {
		t.Fatalf("plain rules changed: %v", got)
	}

	checks := map[string]map[string]any{
		"sylph-e203-0-113-7-p7-r1":      {"outboundTag": "block"},
		"sylph-e203-0-113-7-p7-r2":      {"outboundTag": "_egress-203-0-113-7-v6"},
		"sylph-e203-0-113-7-p7-r3":      {"balancerTag": "bal"},
		"sylph-e203-0-113-7-p7-r4":      {"outboundTag": "_egress-203-0-113-7-direct"},
		"sylph-e203-0-113-7-p7-r5":      {"outboundTag": "de", "network": "tcp,udp"},
		"sylph-e203-0-113-7-p3-default": {"outboundTag": "_egress-203-0-113-7-direct", "network": "tcp,udp"},
	}
	for tag, fields := range checks {
		rule := ruleByTag(t, s, tag)
		for k, v := range fields {
			if rule[k] != v {
				t.Errorf("%s.%s = %v, want %v", tag, k, rule[k], v)
			}
		}
	}
	if users := ruleByTag(t, s, "sylph-e203-0-113-7-p7-r1")["user"]; !slices.Equal(users.([]string), []string{"u-a", "u-b"}) {
		t.Fatalf("p7 users = %v", users)
	}
	if users := ruleByTag(t, s, "sylph-e203-0-113-7-p3-default")["user"]; !slices.Equal(users.([]string), []string{"u-c"}) {
		t.Fatalf("p3 users = %v", users)
	}
	mirror, orig := ruleByTag(t, s, "sylph-e203-0-113-7-p7-r2"), ruleByTag(t, s, "sylph-r2")
	if !slices.Equal(mirror["ip"].([]string), orig["ip"].([]string)) || ruleByTag(t, s, "sylph-e203-0-113-7-p7-r1")["domain"].([]string)[0] != ruleByTag(t, s, "sylph-r1")["domain"].([]string)[0] {
		t.Fatal("mirrored rules lost their conditions")
	}
	if s.Routing["domainStrategy"] != "IPIfNonMatch" {
		t.Fatalf("domain strategy = %v", s.Routing["domainStrategy"])
	}
	for _, r := range s.Routing["rules"].([]any) {
		m := r.(map[string]any)
		if m["outboundTag"] != nil && len(m["outboundTag"].(string)) > 8 && m["outboundTag"].(string)[:8] == "_egress-" && m["user"] == nil {
			t.Fatalf("rule %v sends unscoped traffic to a dedicated address", m)
		}
	}

	bal := egressProfile()
	bal.Default = "bal"
	s, err = Render(RenderInput{Profile: &bal, GeoFile: geoFile, Egress: []Egress{e}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ruleByTag(t, s, "sylph-e203-0-113-7-p3-default"); got["balancerTag"] != "bal" || got["outboundTag"] != nil {
		t.Fatalf("balancer default = %v", got)
	}
}
