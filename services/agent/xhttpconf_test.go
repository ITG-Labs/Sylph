package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const ownerExtraJSON = `{"noSSEHeader":true,"xPaddingObfsMode":true,"xPaddingMethod":"tokenish","xPaddingPlacement":"cookie","xPaddingKey":"_ym_d","seqPlacement":"query","seqKey":"part","uplinkDataPlacement":"body","sessionIDPlacement":"query","sessionIDKey":"sid"}`

func cdnInbound(tag, domain, path string) Inbound {
	return Inbound{
		Tag: tag, Port: 443, Protocol: "vless", Transport: "xhttp", Security: "tls",
		XHTTP: &XHTTP{Path: path, Mode: "auto", ClientMode: "packet-up", Extra: json.RawMessage(ownerExtraJSON)},
		TLS:   &TLS{Domain: domain},
	}
}

func realityXHTTPInbound(tag string) Inbound {
	in := testInbound(tag)
	in.Transport, in.Flow = "xhttp", ""
	in.XHTTP = &XHTTP{Path: "/sync", Mode: "auto", ClientMode: "auto"}
	return in
}

func xhttpSet() []Inbound {
	return []Inbound{testInbound("alpha"), realityXHTTPInbound("rx"), cdnInbound("cdn", "cdn.example.ru", "/api/v2/catalog/sync.json")}
}

func decodeConfig(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func dig(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			l, _ := v.([]any)
			if k >= len(l) {
				return nil
			}
			v = l[k]
		}
	}
	return v
}

func xrayInboundByTag(t *testing.T, cfg map[string]any, tag string) map[string]any {
	t.Helper()
	for _, in := range cfg["inbounds"].([]any) {
		if m := in.(map[string]any); m["tag"] == tag {
			return m
		}
	}
	t.Fatalf("no inbound %s", tag)
	return nil
}

func TestBuildXrayServesXHTTPInbounds(t *testing.T) {
	data, err := BuildXray(xhttpSet(), "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeConfig(t, data)
	alpha := xrayInboundByTag(t, cfg, "alpha")
	if dig(alpha, "streamSettings", "network") != "raw" || dig(alpha, "streamSettings", "xhttpSettings") != nil {
		t.Fatalf("tcp inbound changed: %v", alpha)
	}
	rx := xrayInboundByTag(t, cfg, "rx")
	if dig(rx, "streamSettings", "network") != "xhttp" || dig(rx, "streamSettings", "security") != "reality" ||
		dig(rx, "streamSettings", "sockopt", "acceptProxyProtocol") != true || dig(rx, "streamSettings", "realitySettings", "privateKey") != testPrivateKey ||
		dig(rx, "streamSettings", "xhttpSettings", "path") != "/sync" || dig(rx, "streamSettings", "xhttpSettings", "mode") != "auto" {
		t.Fatalf("reality xhttp inbound = %v", rx)
	}
	cdn := xrayInboundByTag(t, cfg, "cdn")
	stream := cdn["streamSettings"].(map[string]any)
	if cdn["listen"] != "127.0.0.1" || stream["network"] != "xhttp" || stream["security"] != "none" || stream["realitySettings"] != nil {
		t.Fatalf("tls xhttp inbound = %v", cdn)
	}
	if dig(stream, "sockopt", "acceptProxyProtocol") != false || !reflect.DeepEqual(dig(stream, "sockopt", "trustedXForwardedFor"), []any{edgeHeader}) {
		t.Fatalf("tls xhttp sockopt = %v", stream["sockopt"])
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(ownerExtraJSON), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dig(stream, "xhttpSettings", "extra"), want) || dig(stream, "xhttpSettings", "path") != "/api/v2/catalog/sync.json" {
		t.Fatalf("xhttp settings = %v", stream["xhttpSettings"])
	}
}

func caddyL4Routes(cfg map[string]any) []any {
	routes, _ := dig(cfg, "apps", "layer4", "servers", "p443", "routes").([]any)
	return routes
}

func TestBuildCaddyFrontsTLSDomains(t *testing.T) {
	in := append(xhttpSet(), cdnInbound("feed", "cdn.example.ru", "/feed"))
	data, err := BuildCaddy(in, "127.0.0.1:2019")
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeConfig(t, data)
	ports := map[string]float64{}
	sorted, _ := sortedInbounds(in)
	for i, s := range sorted {
		ports[s.Tag] = float64(firstInternalPort + i)
	}
	var front []any
	for _, r := range caddyL4Routes(cfg) {
		if sni := dig(r, "match", 0, "tls", "sni"); reflect.DeepEqual(sni, []any{"cdn.example.ru"}) {
			front = append(front, r)
		}
	}
	if len(front) != 1 || dig(front[0], "handle", 0, "upstreams", 0, "dial", 0) != "127.0.0.1:20000" || dig(front[0], "handle", 0, "proxy_protocol") != "v2" {
		t.Fatalf("layer4 front for the domain = %v", front)
	}
	srv := dig(cfg, "apps", "http", "servers", "sylph-tls").(map[string]any)
	if !reflect.DeepEqual(srv["listen"], []any{"127.0.0.1:20000"}) || dig(srv, "listener_wrappers", 0, "wrapper") != "proxy_protocol" ||
		dig(srv, "listener_wrappers", 1, "wrapper") != "tls" || !reflect.DeepEqual(dig(srv, "tls_connection_policies", 0, "match", "sni"), []any{"cdn.example.ru"}) ||
		!reflect.DeepEqual(srv["protocols"], []any{"h1", "h2"}) || dig(srv, "automatic_https", "disable_redirects") != true {
		t.Fatalf("tls server = %v", srv)
	}
	site := dig(srv, "routes", 0).(map[string]any)
	if !reflect.DeepEqual(dig(site, "match", 0, "vars", "{http.request.tls.server_name}"), []any{"cdn.example.ru"}) || site["terminal"] != true {
		t.Fatalf("site route = %v", site)
	}
	inner := dig(site, "handle", 0, "routes").([]any)
	if len(inner) != 4 {
		t.Fatalf("site routes = %v", inner)
	}
	strip := dig(inner[0], "handle", 0).(map[string]any)
	if dig(inner[0], "match") != nil || strip["handler"] != "headers" || dig(strip, "response", "deferred") != true {
		t.Fatalf("header strip = %v", inner[0])
	}
	for _, h := range []string{"X-Padding", "Via", "Access-Control-Allow-Credentials", "Access-Control-Allow-Origin", "X-Accel-Buffering"} {
		if !strings.Contains(toJSON(dig(strip, "response", "delete")), `"`+h+`"`) {
			t.Fatalf("%s not stripped: %v", h, strip)
		}
	}
	for i, c := range []struct {
		path, tag string
	}{{"/api/v2/catalog/sync.json", "cdn"}, {"/feed", "feed"}} {
		r := inner[i+1]
		proxy := dig(r, "handle", 0).(map[string]any)
		if !reflect.DeepEqual(dig(r, "match", 0, "path"), []any{c.path}) || proxy["handler"] != "reverse_proxy" ||
			dig(proxy, "upstreams", 0, "dial") != "127.0.0.1:"+itoa(ports[c.tag]) || proxy["flush_interval"] != float64(-1) ||
			!reflect.DeepEqual(dig(proxy, "transport", "versions"), []any{"h2c"}) ||
			!reflect.DeepEqual(dig(proxy, "headers", "request", "set", edgeHeader), []any{"1"}) {
			t.Fatalf("path route %s = %v", c.path, r)
		}
		onError := dig(proxy, "handle_response", 0)
		if !reflect.DeepEqual(dig(onError, "match", "status_code"), []any{float64(4), float64(5)}) || dig(onError, "routes", 0, "handle", 0, "handler") != "static_response" {
			t.Fatalf("errors of %s = %v", c.path, onError)
		}
	}
	if dig(inner[3], "match") != nil || dig(inner[3], "handle", 0, "handler") != "static_response" || dig(inner[3], "handle", 0, "status_code") != float64(200) {
		t.Fatalf("stub route = %v", inner[3])
	}
	tlsApp := dig(cfg, "apps", "tls").(map[string]any)
	issuer := dig(tlsApp, "automation", "policies", 0, "issuers", 0).(map[string]any)
	if !reflect.DeepEqual(dig(tlsApp, "certificates", "automate"), []any{"cdn.example.ru"}) || issuer["module"] != "acme" ||
		dig(issuer, "challenges", "http", "disabled") != true || issuer["ca"] != nil || issuer["trusted_roots_pem_files"] != nil {
		t.Fatalf("tls app = %v", tlsApp)
	}
}

func TestBuildCaddyUsesTheGivenACMEServer(t *testing.T) {
	data, err := BuildCaddyWith(CaddyInput{Inbounds: xhttpSet(), AdminListen: "127.0.0.1:2019", ACMECA: "https://pebble:14000/dir", ACMECARoot: "/pebble/root.pem"})
	if err != nil {
		t.Fatal(err)
	}
	issuer := dig(decodeConfig(t, data), "apps", "tls", "automation", "policies", 0, "issuers", 0).(map[string]any)
	if issuer["ca"] != "https://pebble:14000/dir" || !reflect.DeepEqual(issuer["trusted_roots_pem_files"], []any{"/pebble/root.pem"}) {
		t.Fatalf("issuer = %v", issuer)
	}
}

func TestBuildCaddyDecoyProxiesTheSite(t *testing.T) {
	cases := []struct {
		decoy, dial, host, sni string
	}{
		{"https://example.com", "example.com:443", "example.com", "example.com"},
		{"http://example.com:8080", "example.com:8080", "example.com:8080", ""},
	}
	for _, c := range cases {
		in := cdnInbound("cdn", "cdn.example.ru", "/api")
		in.TLS.Decoy = c.decoy
		data, err := BuildCaddy([]Inbound{in}, "127.0.0.1:2019")
		if err != nil {
			t.Fatal(err)
		}
		inner := dig(decodeConfig(t, data), "apps", "http", "servers", "sylph-tls", "routes", 0, "handle", 0, "routes").([]any)
		for _, decoy := range []any{dig(inner[2], "handle", 0), dig(inner[1], "handle", 0, "handle_response", 0, "routes", 0, "handle", 0)} {
			d := decoy.(map[string]any)
			if d["handler"] != "reverse_proxy" || dig(d, "upstreams", 0, "dial") != c.dial || !reflect.DeepEqual(dig(d, "headers", "request", "set", "Host"), []any{c.host}) {
				t.Fatalf("%s: decoy = %v", c.decoy, d)
			}
			if sni, _ := dig(d, "transport", "tls", "server_name").(string); sni != c.sni {
				t.Fatalf("%s: decoy tls = %v", c.decoy, d["transport"])
			}
		}
		if r := dig(inner[1], "handle", 0, "handle_response", 0, "routes", 0, "handle", 0, "rewrite"); !reflect.DeepEqual(r, map[string]any{"method": "GET", "uri": "/"}) {
			t.Fatalf("%s: error decoy rewrite = %v", c.decoy, r)
		}
		if r := dig(inner[2], "handle", 0, "rewrite"); r != nil {
			t.Fatalf("%s: plain decoy rewrites: %v", c.decoy, r)
		}
	}
}

func TestBuildCaddyWithoutTLSInboundsHasNoHTTPApp(t *testing.T) {
	data, err := BuildCaddy([]Inbound{testInbound("alpha"), realityXHTTPInbound("rx")}, "127.0.0.1:2019")
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeConfig(t, data)
	if dig(cfg, "apps", "http") != nil || dig(cfg, "apps", "tls") != nil || len(caddyL4Routes(cfg)) != 3 {
		t.Fatalf("caddy = %s", data)
	}
}

func TestBuildRejectsXHTTPItCannotServe(t *testing.T) {
	cdn := func(f func(*Inbound)) []Inbound {
		in := cdnInbound("cdn", "cdn.example.ru", "/api")
		f(&in)
		return []Inbound{in}
	}
	rx := func(f func(*Inbound)) []Inbound {
		in := realityXHTTPInbound("rx")
		f(&in)
		return []Inbound{in}
	}
	cases := map[string][]Inbound{
		"tcp with xhttp":          {func() Inbound { in := testInbound("a"); in.XHTTP = &XHTTP{Path: "/a", Mode: "auto"}; return in }()},
		"tcp with tls":            {func() Inbound { in := testInbound("a"); in.TLS = &TLS{Domain: "a.example.ru"}; return in }()},
		"xhttp without it":        rx(func(in *Inbound) { in.XHTTP = nil }),
		"reality with tls":        rx(func(in *Inbound) { in.TLS = &TLS{Domain: "a.example.ru"} }),
		"tls without tls":         cdn(func(in *Inbound) { in.TLS = nil }),
		"tls with reality":        cdn(func(in *Inbound) { in.Reality = testInbound("x").Reality }),
		"tls over tcp":            cdn(func(in *Inbound) { in.Transport = "tcp" }),
		"flow on xhttp":           rx(func(in *Inbound) { in.Flow = "xtls-rprx-vision" }),
		"relative path":           cdn(func(in *Inbound) { in.XHTTP.Path = "api" }),
		"path with query":         cdn(func(in *Inbound) { in.XHTTP.Path = "/a?x" }),
		"bad mode":                cdn(func(in *Inbound) { in.XHTTP.Mode = "grpc" }),
		"download settings":       cdn(func(in *Inbound) { in.XHTTP.Extra = json.RawMessage(`{"downloadSettings":{"network":"raw"}}`) }),
		"unknown extra":           cdn(func(in *Inbound) { in.XHTTP.Extra = json.RawMessage(`{"sockopt":{}}`) }),
		"extra not object":        cdn(func(in *Inbound) { in.XHTTP.Extra = json.RawMessage(`[1]`) }),
		"internal domain":         cdn(func(in *Inbound) { in.TLS.Domain = "caddy.internal" }),
		"ip domain":               cdn(func(in *Inbound) { in.TLS.Domain = "10.0.0.1" }),
		"decoy localhost":         cdn(func(in *Inbound) { in.TLS.Decoy = "http://localhost:2019" }),
		"decoy private":           cdn(func(in *Inbound) { in.TLS.Decoy = "https://192.168.1.1" }),
		"decoy scheme":            cdn(func(in *Inbound) { in.TLS.Decoy = "file:///etc/passwd" }),
		"decoy path":              cdn(func(in *Inbound) { in.TLS.Decoy = "https://example.com/x" }),
		"decoy is the domain":     cdn(func(in *Inbound) { in.TLS.Decoy = "https://cdn.example.ru" }),
		"domain is a reality sni": {testInbound("main"), cdnInbound("cdn", "main.example.com", "/a")},
		"overlapping paths":       {cdnInbound("a", "cdn.example.ru", "/api"), cdnInbound("b", "cdn.example.ru", "/api/v2")},
		"different decoys": {cdnInbound("a", "cdn.example.ru", "/a"), func() Inbound {
			in := cdnInbound("b", "cdn.example.ru", "/b")
			in.TLS.Decoy = "https://example.com"
			return in
		}()},
	}
	for name, in := range cases {
		for _, build := range []func([]Inbound) ([]byte, error){
			func(in []Inbound) ([]byte, error) { return BuildXray(in, "127.0.0.1:10085") },
			func(in []Inbound) ([]byte, error) { return BuildCaddy(in, "127.0.0.1:2019") },
		} {
			if _, err := build(in); err == nil {
				t.Fatalf("%s: accepted", name)
			}
		}
	}
	if _, err := BuildCaddy([]Inbound{cdnInbound("a", "cdn.example.ru", "/a"), cdnInbound("b", "cdn.example.ru", "/b"), cdnInbound("c", "other.example.ru", "/a")}, "127.0.0.1:2019"); err != nil {
		t.Fatalf("shared domain with distinct paths: %v", err)
	}
}

func TestCaddyMatchesThePathXrayClientsUse(t *testing.T) {
	cases := []struct {
		path, extra, want string
	}{
		{"/api", "", "/api/*"},
		{"/api/", "", "/api/*"},
		{"/api", `{"sessionIDPlacement":"query"}`, "/api/*"},
		{"/api", `{"sessionIDPlacement":"query","seqPlacement":"cookie"}`, "/api"},
		{"/api", `{"sessionIDPlacement":"header","seqPlacement":"path"}`, "/api/*"},
		{"/api/", `{"sessionIDPlacement":"query","seqPlacement":"query"}`, "/api/"},
	}
	for _, c := range cases {
		x := &XHTTP{Path: c.path, Mode: "auto"}
		if c.extra != "" {
			x.Extra = json.RawMessage(c.extra)
		}
		if got := xrayPathMatch(x); got != c.want {
			t.Errorf("%s %s: match %q, want %q", c.path, c.extra, got, c.want)
		}
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func itoa(f float64) string {
	b, _ := json.Marshal(int(f))
	return string(b)
}
