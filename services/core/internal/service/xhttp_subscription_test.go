package service

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
)

func xhttpSubFixture(t *testing.T) (*subFixture, int64) {
	t.Helper()
	f := newSubFixture(t, nil, nil)
	ctx := context.Background()
	n := f.node(t, "nl", &f.group.ID, true, "main")
	cdn := inbound.Spec{Tag: "cdn", Port: 443, Transport: "xhttp", Security: "tls", Label: "CDN",
		XHTTP: &inbound.XHTTP{Path: "/api/v2/catalog/sync.json", ClientMode: "packet-up", Extra: inbound.Extra{
			NoSSEHeader: true, XPaddingObfsMode: true, XPaddingMethod: "tokenish", XPaddingPlacement: "cookie", XPaddingKey: "_ym_d",
			SeqPlacement: "query", SeqKey: "part", UplinkDataPlacement: "body", SessionIDPlacement: "query", SessionIDKey: "sid",
		}},
		TLS: &inbound.TLS{Domain: "static.example.ru", Address: "185.31.114.248", SNI: "9i2kmuhlma.cdn.twcstorage.ru", ALPN: []string{"h2"}},
	}
	if _, err := f.svc.CreateInbound(ctx, n.ID, cdn); err != nil {
		t.Fatal(err)
	}
	rx := inbound.Spec{Tag: "rx", Port: 443, Transport: "xhttp", Label: "XHTTP",
		Reality: &inbound.Reality{Dest: "www.example.org:443", ServerNames: []string{"rx.example.org"}}, XHTTP: &inbound.XHTTP{Path: "/sync"}}
	if _, err := f.svc.CreateInbound(ctx, n.ID, rx); err != nil {
		t.Fatal(err)
	}
	return f, n.ID
}

func (f *subFixture) viewXHTTP(t *testing.T, xhttp bool) map[string]Connection {
	t.Helper()
	v, err := f.svc.Subscription(context.Background(), SubscriptionRequest{SubToken: f.user.SubToken, UserAgent: "Happ/1.6.0", XHTTP: xhttp})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Connection{}
	for _, c := range v.Connections {
		out[c.Name] = c
	}
	return out
}

func TestSubscriptionXHTTPConnections(t *testing.T) {
	f, _ := xhttpSubFixture(t)
	conns := f.viewXHTTP(t, true)
	if len(conns) != 3 {
		t.Fatalf("connections = %+v", conns)
	}
	c := conns["nl · CDN"]
	if c.Address != "185.31.114.248" || c.Port != 443 || c.Transport != "xhttp" || c.Security != "tls" || c.Flow != "" ||
		c.SNI != "9i2kmuhlma.cdn.twcstorage.ru" || c.Host != "9i2kmuhlma.cdn.twcstorage.ru" || c.Fingerprint != "chrome" ||
		!reflect.DeepEqual(c.ALPN, []string{"h2"}) || c.Path != "/api/v2/catalog/sync.json" || c.Mode != "packet-up" || c.PublicKey != "" {
		t.Fatalf("cdn connection = %+v", c)
	}
	var extra map[string]any
	if err := json.Unmarshal(c.Extra, &extra); err != nil {
		t.Fatal(err)
	}
	if len(extra) != 9 || extra["xPaddingKey"] != "_ym_d" || extra["noSSEHeader"] != nil {
		t.Fatalf("extra = %s", c.Extra)
	}
	r := conns["nl · XHTTP"]
	if r.Address != "nl.example.com" || r.Transport != "xhttp" || r.Security != "reality" || r.Flow != "" || r.Path != "/sync" ||
		r.Mode != "auto" || r.SNI != "rx.example.org" || r.PublicKey == "" || r.ShortID == "" || r.Host != "" || r.Extra != nil {
		t.Fatalf("reality xhttp connection = %+v", r)
	}
	if m := conns["nl · L-main"]; m.Path != "" || m.Extra != nil || m.Flow != "xtls-rprx-vision" {
		t.Fatalf("tcp connection changed: %+v", m)
	}
}

func TestSubscriptionHidesXHTTPFromOldSubsAndOldAgents(t *testing.T) {
	f, nodeID := xhttpSubFixture(t)
	if got := f.viewXHTTP(t, false); len(got) != 1 {
		t.Fatalf("old subs got %+v", got)
	}
	if err := f.svc.store.TouchNodeSync(context.Background(), nodeID, true, false); err != nil {
		t.Fatal(err)
	}
	got := f.viewXHTTP(t, true)
	if _, ok := got["nl"]; len(got) != 1 || !ok {
		t.Fatalf("node with an old agent showed %+v", got)
	}
	if err := f.svc.store.TouchNodeSync(context.Background(), nodeID, true, true); err != nil {
		t.Fatal(err)
	}
	if got := f.viewXHTTP(t, true); len(got) != 3 {
		t.Fatalf("node with a new agent showed %+v", got)
	}
}

func TestCascadeGoesOnlyToTCPReality(t *testing.T) {
	f := setupCascade(t)
	ctx := context.Background()
	if _, err := f.svc.CreateInbound(ctx, f.y.ID, inbound.Spec{Tag: "a-xhttp", Port: 443, Transport: "xhttp",
		Reality: &inbound.Reality{Dest: "www.example.org:443", ServerNames: []string{"a.example.org"}}, XHTTP: &inbound.XHTTP{Path: "/a"}}); err != nil {
		t.Fatal(err)
	}
	resp, err := f.svc.SyncNodeUsers(ctx, f.x.CertCN, -1, AgentCaps{PerInbound: true, XHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range resp.Routing.Outbounds {
		if o.Tag == "to-exit" && (o.Proxy == nil || o.Proxy.SNI != "www.microsoft.com" || o.Proxy.Flow != inbound.FlowVision) {
			t.Fatalf("cascade went to the xhttp inbound: %+v %+v", o, o.Proxy)
		}
	}
}
