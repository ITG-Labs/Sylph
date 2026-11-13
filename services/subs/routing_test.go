package subs

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/clientroute"
)

func realityConn(name string, node int64) Connection {
	return Connection{Name: name, NodeID: node, Address: "berlin.example.com", Port: 443, UUID: "11111111-2222-3333-4444-555555555555",
		Transport: "tcp", Security: "reality", SNI: "www.microsoft.com", PublicKey: "pub", ShortID: "ab", Fingerprint: "chrome", Flow: "xtls-rprx-vision"}
}

func routingView(rules ...clientroute.Rule) *View {
	v := okView()
	v.ClientRouting = &ClientRoutingFeed{UpdatedAt: time.Unix(1794572865, 0), Rules: rules}
	return v
}

type itgLinkRule struct {
	Name       string `json:"name"`
	Action     string `json:"action"`
	ServerName string `json:"server_name"`
}

func itgRules(t *testing.T, link string) []itgLinkRule {
	t.Helper()
	payload, ok := strings.CutPrefix(link, "itgray://rules/import/")
	if !ok {
		t.Fatalf("link = %.40s", link)
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Groups []struct {
			Rules []itgLinkRule `json:"rules"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	return env.Groups[0].Rules
}

func TestRoutingHeaderPerClient(t *testing.T) {
	core := &fakeCore{view: routingView(clientroute.Rule{Name: "ru", Action: clientroute.ActionDirect, Domain: []string{"geosite:category-ru"}})}
	h := newTestServer(core, nil)
	for ua, prefix := range map[string]string{
		"Happ/3.8":          "happ://routing/onadd/",
		"ITGRay/0.2.0":      "itgray://rules/import/",
		"SimpleXray/1.13.0": "simplexray://routing/",
	} {
		rec := get(h, "/"+testToken, map[string]string{"User-Agent": ua})
		if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Routing"), prefix) {
			t.Fatalf("%s: code %d routing %.40q", ua, rec.Code, rec.Header().Get("Routing"))
		}
	}
	if rec := get(h, "/"+testToken, map[string]string{"User-Agent": "v2rayTun/2"}); rec.Header().Get("Routing") != "" {
		t.Fatalf("v2rayTun got routing: %q", rec.Header().Get("Routing"))
	}
}

func TestRoutingHeaderUsesRenderedConnection(t *testing.T) {
	v := routingView(clientroute.Rule{Name: "ai", Action: clientroute.ActionNode, NodeID: 7, Domain: []string{"domain:openai.com"}})
	old := realityConn("Berlin · old", 7)
	old.Security = "none"
	v.Connections = []Connection{old, realityConn("Berlin · main", 7)}
	rec := get(newTestServer(&fakeCore{view: v}, nil), "/"+testToken, map[string]string{"User-Agent": "ITGRay/0.2.0"})
	rules := itgRules(t, rec.Header().Get("Routing"))
	if len(rules) != 1 || rules[0].ServerName != "Berlin · main" || rules[0].Action != "proxy" {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestRoutingHeaderNodeMissing(t *testing.T) {
	v := routingView(clientroute.Rule{Name: "ai", Action: clientroute.ActionNode, NodeID: 9, Domain: []string{"domain:openai.com"}})
	v.Connections = []Connection{realityConn("Berlin", 7)}
	rec := get(newTestServer(&fakeCore{view: v}, nil), "/"+testToken, map[string]string{"User-Agent": "ITGRay/0.2.0"})
	rules := itgRules(t, rec.Header().Get("Routing"))
	if len(rules) != 1 || rules[0].ServerName != "" {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestRoutingHeaderOnStub(t *testing.T) {
	v := routingView(clientroute.Rule{Name: "ai", Action: clientroute.ActionNode, NodeID: 7, Domain: []string{"domain:openai.com"}})
	v.Status = StatusExpired
	v.Connections = []Connection{realityConn("Berlin", 7)}
	rec := get(newTestServer(&fakeCore{view: v}, nil), "/"+testToken, map[string]string{"User-Agent": "ITGRay/0.2.0"})
	if !strings.Contains(stubName(t, rec), "истекла") {
		t.Fatalf("not a stub: %s", rec.Body.String())
	}
	rules := itgRules(t, rec.Header().Get("Routing"))
	if len(rules) != 1 || rules[0].ServerName != "" {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestRoutingHeaderNoFeed(t *testing.T) {
	rec := get(newTestServer(&fakeCore{view: okView()}, nil), "/"+testToken, map[string]string{"User-Agent": "Happ/3.8"})
	if rec.Code != 200 || rec.Header().Get("Routing") != "" {
		t.Fatalf("code %d routing %q", rec.Code, rec.Header().Get("Routing"))
	}
}

func TestRoutingHeaderNothingLeftForClient(t *testing.T) {
	core := &fakeCore{view: routingView(clientroute.Rule{Name: "smtp", Action: clientroute.ActionBlock, Port: "25"})}
	h := newTestServer(core, nil)
	if rec := get(h, "/"+testToken, map[string]string{"User-Agent": "Happ/3.8"}); rec.Header().Get("Routing") != "" {
		t.Fatal("happ cannot express a port rule")
	}
	if rec := get(h, "/"+testToken, map[string]string{"User-Agent": "ITGRay/0.2.0"}); rec.Header().Get("Routing") == "" {
		t.Fatal("itgray expresses a port rule")
	}
}
