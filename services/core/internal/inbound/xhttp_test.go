package inbound

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func ownerExtra() Extra {
	return Extra{
		NoSSEHeader:         true,
		XPaddingObfsMode:    true,
		XPaddingMethod:      "tokenish",
		XPaddingPlacement:   "cookie",
		XPaddingKey:         "_ym_d",
		SeqPlacement:        "query",
		SeqKey:              "part",
		UplinkDataPlacement: "body",
		SessionIDPlacement:  "query",
		SessionIDKey:        "sid",
	}
}

func xhttpTLSSpec() Spec {
	return Spec{
		Tag:       "cdn",
		Port:      443,
		Transport: "xhttp",
		Security:  "tls",
		XHTTP:     &XHTTP{Path: "/api/v2/catalog/sync.json", ClientMode: "packet-up", Extra: ownerExtra()},
		TLS: &TLS{
			Domain:  "static.example.ru",
			Address: "185.31.114.248",
			SNI:     "9i2kmuhlma.cdn.twcstorage.ru",
			Host:    "9i2kmuhlma.cdn.twcstorage.ru",
			ALPN:    []string{"h2"},
		},
	}
}

func xhttpRealitySpec() Spec {
	s := validSpec()
	s.Tag = "reality-xhttp"
	s.Transport = "xhttp"
	s.XHTTP = &XHTTP{Path: "/sync"}
	return s
}

func TestNormalizeXHTTPTLSDefaults(t *testing.T) {
	s := xhttpTLSSpec()
	s.Protocol, s.TLS.Fingerprint, s.TLS.Port, s.XHTTP.Mode = "", "", 0, ""
	s.TLS.Domain = "Static.Example.RU"
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	if s.Protocol != "vless" || s.XHTTP.Mode != "auto" || s.XHTTP.ClientMode != "packet-up" {
		t.Fatalf("defaults = %+v / %+v", s, s.XHTTP)
	}
	if s.TLS.Port != 443 || s.TLS.Fingerprint != "chrome" || s.TLS.Domain != "static.example.ru" {
		t.Fatalf("tls = %+v", s.TLS)
	}
	if s.Reality != nil || Flow(s.Transport, s.Security) != "" {
		t.Fatalf("tls inbound has reality or flow: %+v", s)
	}
}

func TestNormalizeXHTTPRealityKeepsRealityRules(t *testing.T) {
	s := xhttpRealitySpec()
	s.Reality.ShortIDs = nil
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	if s.Reality.PrivateKey == "" || len(s.Reality.ShortIDs) != 1 || s.XHTTP.Mode != "auto" || s.XHTTP.ClientMode != "auto" {
		t.Fatalf("spec = %+v / %+v", s.Reality, s.XHTTP)
	}
	if Flow(s.Transport, s.Security) != "" {
		t.Fatal("vision flow on xhttp")
	}
}

func TestNormalizeRejectsMixedKinds(t *testing.T) {
	cases := []struct {
		name  string
		spec  func() Spec
		field string
	}{
		{"tcp with xhttp settings", func() Spec { s := validSpec(); s.XHTTP = &XHTTP{Path: "/a"}; return s }, "xhttp"},
		{"tcp with tls settings", func() Spec { s := validSpec(); s.TLS = &TLS{Domain: "a.example.com"}; return s }, "tls"},
		{"tls over tcp", func() Spec { s := xhttpTLSSpec(); s.Transport = "tcp"; return s }, "security"},
		{"xhttp without settings", func() Spec { s := xhttpRealitySpec(); s.XHTTP = nil; return s }, "xhttp"},
		{"tls without settings", func() Spec { s := xhttpTLSSpec(); s.TLS = nil; return s }, "tls"},
		{"tls with reality", func() Spec { s := xhttpTLSSpec(); s.Reality = validSpec().Reality; return s }, "reality"},
		{"reality with tls", func() Spec { s := xhttpRealitySpec(); s.TLS = &TLS{Domain: "a.example.com"}; return s }, "tls"},
		{"grpc", func() Spec { s := validSpec(); s.Transport = "grpc"; return s }, "transport"},
	}
	for _, c := range cases {
		s := c.spec()
		if got := fieldOf(t, Normalize(&s)); got != c.field {
			t.Errorf("%s: field = %q, want %q", c.name, got, c.field)
		}
	}
}

func TestNormalizeRejectsXHTTP(t *testing.T) {
	packetUp := func(x *XHTTP) { x.Mode, x.ClientMode = "packet-up", "packet-up" }
	cases := []struct {
		name  string
		edit  func(*XHTTP)
		field string
	}{
		{"empty path", func(x *XHTTP) { x.Path = "" }, "xhttp.path"},
		{"relative path", func(x *XHTTP) { x.Path = "api" }, "xhttp.path"},
		{"path with query", func(x *XHTTP) { x.Path = "/a?b=1" }, "xhttp.path"},
		{"path with space", func(x *XHTTP) { x.Path = "/a b" }, "xhttp.path"},
		{"path with wildcard", func(x *XHTTP) { x.Path = "/api*" }, "xhttp.path"},
		{"path with escape", func(x *XHTTP) { x.Path = "/a%2Fb" }, "xhttp.path"},
		{"long path", func(x *XHTTP) { x.Path = "/" + strings.Repeat("a", 200) }, "xhttp.path"},
		{"bad mode", func(x *XHTTP) { x.Mode = "stream" }, "xhttp.mode"},
		{"bad client mode", func(x *XHTTP) { x.ClientMode = "grpc" }, "xhttp.client_mode"},
		{"host header", func(x *XHTTP) { x.Extra.Headers = map[string]string{"Host": "a.com"} }, "xhttp.extra.headers"},
		{"bad header name", func(x *XHTTP) { x.Extra.Headers = map[string]string{"X Y": "1"} }, "xhttp.extra.headers"},
		{"header value newline", func(x *XHTTP) { x.Extra.Headers = map[string]string{"X-Y": "1\r\nZ: 2"} }, "xhttp.extra.headers"},
		{"padding bytes zero", func(x *XHTTP) { x.Extra.XPaddingBytes = "0-100" }, "xhttp.extra.xPaddingBytes"},
		{"padding bytes reversed", func(x *XHTTP) { x.Extra.XPaddingBytes = "200-100" }, "xhttp.extra.xPaddingBytes"},
		{"padding bytes garbage", func(x *XHTTP) { x.Extra.XPaddingBytes = "lots" }, "xhttp.extra.xPaddingBytes"},
		{"padding bytes overflow", func(x *XHTTP) { x.Extra.XPaddingBytes = "1-99999999999" }, "xhttp.extra.xPaddingBytes"},
		{"padding placement", func(x *XHTTP) { x.Extra.XPaddingPlacement = "body" }, "xhttp.extra.xPaddingPlacement"},
		{"padding method", func(x *XHTTP) { x.Extra.XPaddingMethod = "random" }, "xhttp.extra.xPaddingMethod"},
		{"padding key", func(x *XHTTP) { x.Extra.XPaddingKey = "a;b" }, "xhttp.extra.xPaddingKey"},
		{"padding header", func(x *XHTTP) { x.Extra.XPaddingHeader = "X:Pad" }, "xhttp.extra.xPaddingHeader"},
		{"session placement", func(x *XHTTP) { x.Extra.SessionIDPlacement = "body" }, "xhttp.extra.sessionIDPlacement"},
		{"session key", func(x *XHTTP) { x.Extra.SessionIDKey = "s id" }, "xhttp.extra.sessionIDKey"},
		{"seq placement", func(x *XHTTP) { x.Extra.SeqPlacement = "fragment" }, "xhttp.extra.seqPlacement"},
		{"seq key", func(x *XHTTP) { x.Extra.SeqKey = "p=1" }, "xhttp.extra.seqKey"},
		{"uplink placement", func(x *XHTTP) { x.Extra.UplinkDataPlacement = "query" }, "xhttp.extra.uplinkDataPlacement"},
		{"cookie uplink outside packet-up", func(x *XHTTP) { x.Extra.UplinkDataPlacement = "cookie" }, "xhttp.extra.uplinkDataPlacement"},
		{"cookie uplink with server auto", func(x *XHTTP) { x.ClientMode = "packet-up"; x.Extra.UplinkDataPlacement = "header" }, "xhttp.extra.uplinkDataPlacement"},
		{"uplink key", func(x *XHTTP) { x.Extra.UplinkDataKey = "a b" }, "xhttp.extra.uplinkDataKey"},
		{"get uplink outside packet-up", func(x *XHTTP) { x.Extra.UplinkHTTPMethod = "GET" }, "xhttp.extra.uplinkHTTPMethod"},
		{"bad method", func(x *XHTTP) { x.Extra.UplinkHTTPMethod = "PO ST" }, "xhttp.extra.uplinkHTTPMethod"},
		{"tiny session table", func(x *XHTTP) { x.Extra.SessionIDTable = "hex"; x.Extra.SessionIDLength = "4" }, "xhttp.extra.sessionIDTable"},
		{"table without length", func(x *XHTTP) { x.Extra.SessionIDTable = "Base62" }, "xhttp.extra.sessionIDLength"},
		{"endless session length", func(x *XHTTP) { x.Extra.SessionIDTable = "hex"; x.Extra.SessionIDLength = "1-2147483647" }, "xhttp.extra.sessionIDLength"},
		{"long session length alone", func(x *XHTTP) { x.Extra.SessionIDLength = "200" }, "xhttp.extra.sessionIDLength"},
		{"non ascii table", func(x *XHTTP) {
			x.Extra.SessionIDTable = "абвгдеёжзийклмнопрстуфхцчшщъыьэюя"
			x.Extra.SessionIDLength = "16"
		}, "xhttp.extra.sessionIDTable"},
		{"chunk size", func(x *XHTTP) { x.Extra.UplinkChunkSize = "-1" }, "xhttp.extra.uplinkChunkSize"},
		{"post bytes", func(x *XHTTP) { x.Extra.ScMaxEachPostBytes = "x" }, "xhttp.extra.scMaxEachPostBytes"},
		{"post interval", func(x *XHTTP) { x.Extra.ScMinPostsIntervalMs = "30-10" }, "xhttp.extra.scMinPostsIntervalMs"},
		{"stream up secs", func(x *XHTTP) { x.Extra.ScStreamUpServerSecs = "a-b" }, "xhttp.extra.scStreamUpServerSecs"},
		{"buffered posts", func(x *XHTTP) { x.Extra.ScMaxBufferedPosts = -1 }, "xhttp.extra.scMaxBufferedPosts"},
		{"header bytes", func(x *XHTTP) { x.Extra.ServerMaxHeaderBytes = -1 }, "xhttp.extra.serverMaxHeaderBytes"},
		{"xmux both limits", func(x *XHTTP) { x.Extra.Xmux = &Xmux{MaxConcurrency: "16-32", MaxConnections: "4"} }, "xhttp.extra.xmux"},
		{"xmux bad range", func(x *XHTTP) { x.Extra.Xmux = &Xmux{HMaxRequestTimes: "many"} }, "xhttp.extra.xmux.hMaxRequestTimes"},
		{"xmux keepalive", func(x *XHTTP) { x.Extra.Xmux = &Xmux{HKeepAlivePeriod: -2} }, "xhttp.extra.xmux.hKeepAlivePeriod"},
	}
	for _, c := range cases {
		s := xhttpTLSSpec()
		s.XHTTP.ClientMode = ""
		c.edit(s.XHTTP)
		if got := fieldOf(t, Normalize(&s)); got != c.field {
			t.Errorf("%s: field = %q, want %q", c.name, got, c.field)
		}
	}
	s := xhttpTLSSpec()
	packetUp(s.XHTTP)
	s.XHTTP.Extra.UplinkDataPlacement, s.XHTTP.Extra.UplinkHTTPMethod = "cookie", "get"
	s.XHTTP.Extra.SessionIDTable, s.XHTTP.Extra.SessionIDLength = "Base62", "16-24"
	if err := Normalize(&s); err != nil {
		t.Fatalf("packet-up only settings: %v", err)
	}
	if s.XHTTP.Extra.UplinkHTTPMethod != "GET" {
		t.Fatalf("method not uppercased: %q", s.XHTTP.Extra.UplinkHTTPMethod)
	}
}

func TestNormalizeRejectsTLS(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*TLS)
		field string
	}{
		{"empty domain", func(t *TLS) { t.Domain = "" }, "tls.domain"},
		{"ip domain", func(t *TLS) { t.Domain = "5.6.7.8" }, "tls.domain"},
		{"dotless domain", func(t *TLS) { t.Domain = "localhost" }, "tls.domain"},
		{"internal domain", func(t *TLS) { t.Domain = "node.internal" }, "tls.domain"},
		{"decoy ftp", func(t *TLS) { t.Decoy = "ftp://example.com" }, "tls.decoy"},
		{"decoy localhost", func(t *TLS) { t.Decoy = "http://localhost:2019" }, "tls.decoy"},
		{"decoy private ip", func(t *TLS) { t.Decoy = "https://10.0.0.1" }, "tls.decoy"},
		{"decoy documentation ip", func(t *TLS) { t.Decoy = "https://203.0.113.5" }, "tls.decoy"},
		{"decoy benchmark ip", func(t *TLS) { t.Decoy = "http://198.18.0.1:8080" }, "tls.decoy"},
		{"decoy with path", func(t *TLS) { t.Decoy = "https://example.com/landing" }, "tls.decoy"},
		{"decoy with query", func(t *TLS) { t.Decoy = "https://example.com/?a=1" }, "tls.decoy"},
		{"decoy with user", func(t *TLS) { t.Decoy = "https://u:p@example.com" }, "tls.decoy"},
		{"decoy bad port", func(t *TLS) { t.Decoy = "https://example.com:0" }, "tls.decoy"},
		{"internal address", func(t *TLS) { t.Address = "192.168.1.10" }, "tls.address"},
		{"bad address", func(t *TLS) { t.Address = "-x-.com" }, "tls.address"},
		{"port too big", func(t *TLS) { t.Port = 70000 }, "tls.port"},
		{"ip sni", func(t *TLS) { t.SNI = "1.2.3.4" }, "tls.sni"},
		{"bad host", func(t *TLS) { t.Host = "a b.com" }, "tls.host"},
		{"bad alpn", func(t *TLS) { t.ALPN = []string{"spdy/3"} }, "tls.alpn"},
		{"duplicate alpn", func(t *TLS) { t.ALPN = []string{"h2", "h2"} }, "tls.alpn"},
		{"h3 straight to the node", func(t *TLS) { t.Address = ""; t.ALPN = []string{"h3", "h2"} }, "tls.alpn"},
		{"h3 to the node's own domain", func(t *TLS) { t.Address = "Static.Example.RU"; t.ALPN = []string{"h3"} }, "tls.alpn"},
		{"bad fingerprint", func(t *TLS) { t.Fingerprint = "netscape" }, "tls.fingerprint"},
	}
	for _, c := range cases {
		s := xhttpTLSSpec()
		c.edit(s.TLS)
		if got := fieldOf(t, Normalize(&s)); got != c.field {
			t.Errorf("%s: field = %q, want %q", c.name, got, c.field)
		}
	}
}

func TestDomainsLoseTheTrailingDot(t *testing.T) {
	s := xhttpTLSSpec()
	s.TLS.Domain, s.TLS.SNI, s.TLS.Host, s.TLS.Address = "Static.Example.RU.", "X.cdn.example.net.", "x.cdn.example.net.", "Edge.Example.NET."
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	if s.TLS.Domain != "static.example.ru" || s.TLS.SNI != "x.cdn.example.net" || s.TLS.Host != "x.cdn.example.net" || s.TLS.Address != "edge.example.net" {
		t.Fatalf("tls = %+v", s.TLS)
	}
	ok := xhttpTLSSpec()
	ok.TLS.ALPN = []string{"h3", "h2"}
	if err := Normalize(&ok); err != nil {
		t.Fatalf("h3 through a cdn refused: %v", err)
	}
}

func TestDecoyIsStoredAsOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com./":   "https://example.com",
		"https://Example.COM":     "https://example.com",
		"https://example.com/":    "https://example.com",
		"http://example.com:8080": "http://example.com:8080",
		"":                        "",
	} {
		s := xhttpTLSSpec()
		s.TLS.Decoy = in
		if err := Normalize(&s); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if s.TLS.Decoy != want {
			t.Errorf("%q stored as %q, want %q", in, s.TLS.Decoy, want)
		}
	}
}

func TestClientEndpointDefaults(t *testing.T) {
	cases := []struct {
		name string
		tls  TLS
		want Endpoint
	}{
		{"direct", TLS{Domain: "o.example.com", Port: 443}, Endpoint{"o.example.com", 443, "o.example.com", "o.example.com"}},
		{"cdn by ip", TLS{Domain: "o.example.com", Port: 443, Address: "185.31.114.248", SNI: "x.cdn.example.net"}, Endpoint{"185.31.114.248", 443, "x.cdn.example.net", "x.cdn.example.net"}},
		{"cdn by name", TLS{Domain: "o.example.com", Port: 8443, Address: "x.cdn.example.net"}, Endpoint{"x.cdn.example.net", 8443, "x.cdn.example.net", "x.cdn.example.net"}},
		{"ip without sni", TLS{Domain: "o.example.com", Port: 443, Address: "185.31.114.248"}, Endpoint{"185.31.114.248", 443, "o.example.com", "o.example.com"}},
		{"own host", TLS{Domain: "o.example.com", Port: 443, SNI: "a.example.net", Host: "b.example.net"}, Endpoint{"o.example.com", 443, "a.example.net", "b.example.net"}},
	}
	for _, c := range cases {
		if got := c.tls.Client(); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestLinkExtraDropsServerOnlyFields(t *testing.T) {
	x := XHTTP{Path: "/p", Extra: ownerExtra()}
	x.Extra.ScMaxBufferedPosts, x.Extra.ScStreamUpServerSecs, x.Extra.ServerMaxHeaderBytes = 30, "20-80", 8192
	x.Extra.ScMinPostsIntervalMs, x.Extra.Xmux = "10-50", &Xmux{MaxConnections: "2"}
	got := x.LinkExtra()
	want := ownerExtra()
	want.NoSSEHeader = false
	want.ScMinPostsIntervalMs, want.Xmux = "10-50", &Xmux{MaxConnections: "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("link extra = %+v\nwant %+v", got, want)
	}
	if x.Extra.ServerMaxHeaderBytes != 8192 || !x.Extra.NoSSEHeader {
		t.Fatal("LinkExtra changed the inbound")
	}
}

func TestExtraJSONUsesXrayNamesAndOmitsDefaults(t *testing.T) {
	raw, err := json.Marshal(ownerExtra())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"xPaddingObfsMode":true,"xPaddingKey":"_ym_d","xPaddingPlacement":"cookie","xPaddingMethod":"tokenish","sessionIDPlacement":"query","sessionIDKey":"sid","seqPlacement":"query","seqKey":"part","uplinkDataPlacement":"body","noSSEHeader":true}`
	if string(raw) != want {
		t.Fatalf("json = %s\nwant %s", raw, want)
	}
	if raw, _ := json.Marshal(Extra{}); string(raw) != "{}" {
		t.Fatalf("empty extra = %s", raw)
	}
}

func TestPathsOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"/api", "/api", true},
		{"/api", "/api/", true},
		{"/api", "/api/v2", true},
		{"/api/v2", "/api", true},
		{"/", "/anything", true},
		{"/api", "/apix", false},
		{"/a/b", "/a/c", false},
	}
	for _, c := range cases {
		if got := PathsOverlap(c.a, c.b); got != c.want {
			t.Errorf("PathsOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if got := PathPrefix("/api/v2/catalog/sync.json"); got != "/api/v2/catalog/sync.json/" {
		t.Fatalf("prefix = %q", got)
	}
}

func TestSNINames(t *testing.T) {
	tls := xhttpTLSSpec()
	if got := SNINames(&tls); !reflect.DeepEqual(got, []string{"static.example.ru"}) {
		t.Fatalf("tls names = %v", got)
	}
	r := xhttpRealitySpec()
	if got := SNINames(&r); !reflect.DeepEqual(got, []string{"www.example.com"}) {
		t.Fatalf("reality names = %v", got)
	}
}

func TestNormalizeRefusesModesTheServerWouldReject(t *testing.T) {
	cases := []struct {
		reality        bool
		server, client string
		ok             bool
	}{
		{false, "auto", "auto", true},
		{false, "packet-up", "auto", true},
		{false, "stream-one", "auto", false},
		{false, "stream-up", "stream-one", true},
		{false, "stream-up", "stream-up", true},
		{false, "packet-up", "stream-up", false},
		{false, "stream-one", "stream-up", false},
		{false, "stream-up", "packet-up", false},
		{true, "packet-up", "auto", false},
		{true, "stream-up", "auto", true},
		{true, "stream-one", "auto", true},
		{true, "packet-up", "packet-up", true},
	}
	for _, c := range cases {
		s := xhttpTLSSpec()
		if c.reality {
			s = xhttpRealitySpec()
		}
		s.XHTTP.Mode, s.XHTTP.ClientMode = c.server, c.client
		err := Normalize(&s)
		if c.ok && err != nil {
			t.Errorf("reality=%v %s/%s refused: %v", c.reality, c.server, c.client, err)
		}
		if !c.ok && (err == nil || fieldOf(t, err) != "xhttp.client_mode") {
			t.Errorf("reality=%v %s/%s: err = %v, want xhttp.client_mode", c.reality, c.server, c.client, err)
		}
	}
}

func TestRealityDestRefusesAddressesTheAgentRefuses(t *testing.T) {
	for _, dest := range []string{"192.0.2.10:443", "198.51.100.7:443", "203.0.113.9:443", "198.19.1.1:443", "192.88.99.1:443", "192.0.0.8:443", "240.0.0.1:443", "0.1.2.3:443"} {
		s := validSpec()
		s.Reality.Dest = dest
		if got := fieldOf(t, Normalize(&s)); got != "reality.dest" {
			t.Errorf("%s: field = %q", dest, got)
		}
	}
}
