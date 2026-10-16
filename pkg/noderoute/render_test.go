package noderoute

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
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
		if len(ips) != 1 || ips[0] != "geoip:private" {
			t.Fatalf("guard ips = %v", first["ip"])
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
