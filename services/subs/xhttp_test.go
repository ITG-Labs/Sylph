package subs

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const ownerExtra = `{"xPaddingObfsMode":true,"xPaddingKey":"_ym_d","xPaddingPlacement":"cookie","xPaddingMethod":"tokenish","sessionIDPlacement":"query","sessionIDKey":"sid","seqPlacement":"query","seqKey":"part","uplinkDataPlacement":"body"}`

func cdnConnection() Connection {
	return Connection{
		Name: "TW-CDN", Address: "185.31.114.248", Port: 443, UUID: "497d3ffc-c902-482a-860f-0144944fe983",
		Transport: "xhttp", Security: "tls", SNI: "9i2kmuhlma.cdn.twcstorage.ru", Host: "9i2kmuhlma.cdn.twcstorage.ru",
		Fingerprint: "chrome", ALPN: []string{"h2"}, Path: "/api/v2/catalog/sync.json", Mode: "packet-up", Extra: json.RawMessage(ownerExtra),
	}
}

func realityXHTTPConnection() Connection {
	return Connection{
		Name: "NL XHTTP", Address: "nl1.example.com", Port: 443, UUID: "091192b5-1fd3-4aeb-9733-810ef24c2964",
		Transport: "xhttp", Security: "reality", SNI: "www.apple.com", PublicKey: "EznkPne5ZLOrLvDbqseDQycq-ca9v1kEytFwD1-fy3s",
		ShortID: "98b7a90e659d766f", Fingerprint: "chrome", Path: "/sync", Mode: "auto",
	}
}

func linkQuery(t *testing.T, c Connection) (*url.URL, url.Values) {
	t.Helper()
	u, err := url.Parse(vlessLink(c))
	if err != nil {
		t.Fatal(err)
	}
	return u, u.Query()
}

func TestVlessLinkCarriesXHTTPOverTLS(t *testing.T) {
	u, q := linkQuery(t, cdnConnection())
	if u.Host != "185.31.114.248:443" || u.User.Username() != "497d3ffc-c902-482a-860f-0144944fe983" || u.Fragment != "TW-CDN" {
		t.Fatalf("link = %s", u)
	}
	want := map[string]string{
		"encryption": "none", "type": "xhttp", "security": "tls", "sni": "9i2kmuhlma.cdn.twcstorage.ru", "host": "9i2kmuhlma.cdn.twcstorage.ru",
		"fp": "chrome", "alpn": "h2", "path": "/api/v2/catalog/sync.json", "mode": "packet-up",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	var got, extra map[string]any
	if err := json.Unmarshal([]byte(q.Get("extra")), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(ownerExtra), &extra); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, extra) {
		t.Fatalf("extra = %s", q.Get("extra"))
	}
	for _, k := range []string{"flow", "pbk", "sid", "support-x25519mlkem768"} {
		if q.Has(k) {
			t.Errorf("tls link carries %s", k)
		}
	}
}

func TestVlessLinkCarriesXHTTPOverReality(t *testing.T) {
	_, q := linkQuery(t, realityXHTTPConnection())
	want := map[string]string{
		"type": "xhttp", "security": "reality", "sni": "www.apple.com", "fp": "chrome", "pbk": "EznkPne5ZLOrLvDbqseDQycq-ca9v1kEytFwD1-fy3s",
		"sid": "98b7a90e659d766f", "path": "/sync", "mode": "auto", "support-x25519mlkem768": "true",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	for _, k := range []string{"flow", "host", "extra", "alpn"} {
		if q.Has(k) {
			t.Errorf("reality xhttp link carries %s", k)
		}
	}
}

func TestRenderClashCarriesXHTTP(t *testing.T) {
	full := cdnConnection()
	full.Extra = json.RawMessage(`{"headers":{"X-App":"1"},"xPaddingBytes":"100-1000","xPaddingObfsMode":true,"xPaddingKey":"_ym_d","xPaddingHeader":"X-Pad","xPaddingPlacement":"header","xPaddingMethod":"tokenish","uplinkHTTPMethod":"PUT","sessionIDPlacement":"cookie","sessionIDKey":"sid","sessionIDTable":"Base62","sessionIDLength":"16-24","seqPlacement":"header","seqKey":"X-Seq","uplinkDataPlacement":"cookie","uplinkDataKey":"d","uplinkChunkSize":"1024-4096","noGRPCHeader":true,"scMaxEachPostBytes":"500000-1000000","scMinPostsIntervalMs":"10-50","xmux":{"maxConnections":"2-4","cMaxReuseTimes":"64-128","hMaxRequestTimes":"600-900","hMaxReusableSecs":"1800-3000","hKeepAlivePeriod":30}}`)
	data, err := RenderClash([]Connection{full, realityXHTTPConnection()})
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	cdn, rx := profile.Proxies[0], profile.Proxies[1]
	if cdn["network"] != "xhttp" || cdn["tls"] != true || cdn["servername"] != "9i2kmuhlma.cdn.twcstorage.ru" || cdn["client-fingerprint"] != "chrome" ||
		!reflect.DeepEqual(cdn["alpn"], []any{"h2"}) || cdn["flow"] != nil || cdn["reality-opts"] != nil {
		t.Fatalf("cdn proxy = %v", cdn)
	}
	wantOpts := map[string]any{
		"path": "/api/v2/catalog/sync.json", "host": "9i2kmuhlma.cdn.twcstorage.ru", "mode": "packet-up", "headers": map[string]any{"X-App": "1"},
		"no-grpc-header": true, "x-padding-bytes": "100-1000", "x-padding-obfs-mode": true, "x-padding-key": "_ym_d", "x-padding-header": "X-Pad",
		"x-padding-placement": "header", "x-padding-method": "tokenish", "uplink-http-method": "PUT", "session-placement": "cookie", "session-key": "sid",
		"session-table": "Base62", "session-length": "16-24", "seq-placement": "header", "seq-key": "X-Seq", "uplink-data-placement": "cookie",
		"uplink-data-key": "d", "uplink-chunk-size": "1024-4096", "sc-max-each-post-bytes": "500000-1000000", "sc-min-posts-interval-ms": "10-50",
		"reuse-settings": map[string]any{"max-connections": "2-4", "c-max-reuse-times": "64-128", "h-max-request-times": "600-900", "h-max-reusable-secs": "1800-3000", "h-keep-alive-period": 30},
	}
	if !reflect.DeepEqual(cdn["xhttp-opts"], wantOpts) {
		t.Fatalf("xhttp-opts = %v\nwant %v", cdn["xhttp-opts"], wantOpts)
	}
	if rx["network"] != "xhttp" || rx["reality-opts"] == nil || rx["servername"] != "www.apple.com" || rx["flow"] != nil ||
		!reflect.DeepEqual(rx["xhttp-opts"], map[string]any{"path": "/sync", "mode": "auto"}) {
		t.Fatalf("reality xhttp proxy = %v", rx)
	}
}

func TestContentKeepsXHTTPWhereClientsUnderstandIt(t *testing.T) {
	view := &View{Status: StatusOK, Connections: append(goldenConnections(), cdnConnection(), realityXHTTPConnection())}
	for _, f := range []Format{FormatBase64, FormatClash} {
		conns, stub := content(view, f)
		if stub != "" || len(conns) != 4 {
			t.Fatalf("%s: %d connections, stub %q", f, len(conns), stub)
		}
	}
	if _, stub := content(view, FormatSingBox); stub != StubSingBox {
		t.Fatalf("sing-box stub = %q", stub)
	}
	odd := &View{Status: StatusOK, Connections: []Connection{{Name: "x", Transport: "grpc", Security: "tls"}}}
	if _, stub := content(odd, FormatBase64); stub != StubNoServers {
		t.Fatalf("unknown transport kept: %q", stub)
	}
}

func TestLinksForTCPAreUnchanged(t *testing.T) {
	links := string(decodeBase64(t, RenderBase64(goldenConnections())))
	if strings.Contains(links, "path=") || strings.Contains(links, "extra=") || strings.Contains(links, "mode=") {
		t.Fatalf("tcp links gained xhttp fields: %s", links)
	}
}
