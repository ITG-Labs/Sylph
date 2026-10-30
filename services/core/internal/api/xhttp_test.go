package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

type xhttpInboundView struct {
	ID        int64  `json:"id"`
	Transport string `json:"transport"`
	Security  string `json:"security"`
	Flow      string `json:"flow"`
	XHTTP     *struct {
		Path       string         `json:"path"`
		Mode       string         `json:"mode"`
		ClientMode string         `json:"client_mode"`
		Extra      map[string]any `json:"extra"`
	} `json:"xhttp"`
	TLS *struct {
		Domain      string   `json:"domain"`
		Decoy       string   `json:"decoy"`
		Address     string   `json:"address"`
		Port        int      `json:"port"`
		SNI         string   `json:"sni"`
		Host        string   `json:"host"`
		ALPN        []string `json:"alpn"`
		Fingerprint string   `json:"fingerprint"`
	} `json:"tls"`
	Reality *struct {
		PublicKey string `json:"public_key"`
	} `json:"reality"`
}

func ownerExtraBody() map[string]any {
	return map[string]any{
		"noSSEHeader": true, "xPaddingObfsMode": true, "xPaddingMethod": "tokenish", "xPaddingPlacement": "cookie",
		"xPaddingKey": "_ym_d", "seqPlacement": "query", "seqKey": "part", "uplinkDataPlacement": "body",
		"sessionIDPlacement": "query", "sessionIDKey": "sid",
	}
}

func tlsBody(tag, domain, path string) map[string]any {
	return map[string]any{
		"tag": tag, "port": 443, "transport": "xhttp", "security": "tls", "label": "CDN",
		"xhttp": map[string]any{"path": path, "client_mode": "packet-up", "extra": ownerExtraBody()},
		"tls": map[string]any{
			"domain": domain, "address": "185.31.114.248", "sni": "9i2kmuhlma.cdn.twcstorage.ru",
			"host": "9i2kmuhlma.cdn.twcstorage.ru", "alpn": []string{"h2"},
		},
	}
}

func xhttpRealityBody(tag, sni string) map[string]any {
	b := realityBody(tag, sni)
	b["transport"] = "xhttp"
	b["xhttp"] = map[string]any{"path": "/sync"}
	return b
}

func TestAdminXHTTPInboundLifecycle(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-1", nil)
	path := fmt.Sprintf("/v1/admin/nodes/%d/inbounds", nodeID)
	r := e.call(t, http.MethodPost, path, tlsBody("cdn", "Static.Example.RU", "/api/v2/catalog/sync.json"))
	mustStatus(t, r, http.StatusOK)
	var in xhttpInboundView
	r.decode(t, &in)
	if in.Transport != "xhttp" || in.Security != "tls" || in.Flow != "" || in.Reality != nil || in.XHTTP == nil || in.TLS == nil {
		t.Fatalf("inbound = %s", r.body)
	}
	if in.XHTTP.Mode != "auto" || in.XHTTP.ClientMode != "packet-up" || in.XHTTP.Extra["xPaddingKey"] != "_ym_d" || len(in.XHTTP.Extra) != 10 {
		t.Fatalf("xhttp = %+v", in.XHTTP)
	}
	if in.TLS.Domain != "static.example.ru" || in.TLS.Port != 443 || in.TLS.Fingerprint != "chrome" || in.TLS.SNI != "9i2kmuhlma.cdn.twcstorage.ru" {
		t.Fatalf("tls = %+v", in.TLS)
	}
	r = e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), map[string]any{
		"xhttp": map[string]any{"path": "/feed", "mode": "packet-up", "client_mode": "packet-up", "extra": map[string]any{"uplinkDataPlacement": "cookie"}},
		"tls":   map[string]any{"domain": "static.example.ru", "decoy": "https://Example.com/"},
	})
	mustStatus(t, r, http.StatusOK)
	in = xhttpInboundView{}
	r.decode(t, &in)
	if in.XHTTP.Path != "/feed" || len(in.XHTTP.Extra) != 1 || in.TLS.Decoy != "https://example.com" || in.TLS.Address != "" || in.TLS.SNI != "" {
		t.Fatalf("xhttp and tls must be replaced whole: %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), map[string]any{"reality": map[string]any{"fingerprint": "firefox"}}), http.StatusBadRequest, "bad_request", "reality")
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), map[string]any{"xhttp": map[string]any{"path": "nope"}}), http.StatusBadRequest, "bad_request", "xhttp.path")

	r = e.call(t, http.MethodPost, path, xhttpRealityBody("rx", "www.example.com"))
	mustStatus(t, r, http.StatusOK)
	var rx xhttpInboundView
	r.decode(t, &rx)
	if rx.Transport != "xhttp" || rx.Security != "reality" || rx.Flow != "" || rx.Reality == nil || rx.Reality.PublicKey == "" || rx.TLS != nil || rx.XHTTP.Path != "/sync" {
		t.Fatalf("xhttp reality = %s", r.body)
	}
	if bytes.Contains(r.body, []byte("private_key")) {
		t.Fatalf("private key exposed: %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", rx.ID), map[string]any{"tls": map[string]any{"domain": "a.example.com"}}), http.StatusBadRequest, "bad_request", "tls")
	tcp := e.call(t, http.MethodPost, path, realityBody("tcp", "www.tcp.com"))
	mustStatus(t, tcp, http.StatusOK)
	var tcpIn xhttpInboundView
	tcp.decode(t, &tcpIn)
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", tcpIn.ID), map[string]any{"xhttp": map[string]any{"path": "/a"}}), http.StatusBadRequest, "bad_request", "xhttp")
}

func TestAdminXHTTPConflictsOnANode(t *testing.T) {
	e := newAdminEnv(t)
	n1 := e.createNode(t, "node-1", nil)
	n2 := e.createNode(t, "node-2", nil)
	path1 := fmt.Sprintf("/v1/admin/nodes/%d/inbounds", n1)
	mustStatus(t, e.call(t, http.MethodPost, path1, realityBody("tcp", "www.example.com")), http.StatusOK)
	mustField(t, e.call(t, http.MethodPost, path1, tlsBody("cdn", "www.example.com", "/a")), http.StatusConflict, "conflict", "tls.domain")
	mustStatus(t, e.call(t, http.MethodPost, path1, tlsBody("cdn", "static.example.ru", "/api/v2")), http.StatusOK)
	mustField(t, e.call(t, http.MethodPost, path1, realityBody("tcp2", "static.example.ru")), http.StatusConflict, "conflict", "reality.server_names")
	mustField(t, e.call(t, http.MethodPost, path1, xhttpRealityBody("rx", "static.example.ru")), http.StatusConflict, "conflict", "reality.server_names")
	mustField(t, e.call(t, http.MethodPost, path1, tlsBody("cdn2", "static.example.ru", "/api/v2/catalog")), http.StatusConflict, "conflict", "xhttp.path")
	mustField(t, e.call(t, http.MethodPost, path1, tlsBody("cdn2", "static.example.ru", "/api")), http.StatusConflict, "conflict", "xhttp.path")
	other := tlsBody("cdn2", "static.example.ru", "/feed")
	other["tls"].(map[string]any)["decoy"] = "https://example.com"
	mustField(t, e.call(t, http.MethodPost, path1, other), http.StatusConflict, "conflict", "tls.decoy")
	r := e.call(t, http.MethodPost, path1, tlsBody("cdn2", "static.example.ru", "/feed"))
	mustStatus(t, r, http.StatusOK)
	var second xhttpInboundView
	r.decode(t, &second)
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", second.ID), map[string]any{
		"xhttp": map[string]any{"path": "/api/v2/x"}, "tls": map[string]any{"domain": "static.example.ru"},
	}), http.StatusConflict, "conflict", "xhttp.path")
	mustStatus(t, e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", n2), tlsBody("cdn", "static.example.ru", "/api/v2")), http.StatusOK)
}

func TestAdminDecoyMustNotLoopIntoOurNodes(t *testing.T) {
	e := newAdminEnv(t)
	n1 := e.createNode(t, "node-1", nil)
	path1 := fmt.Sprintf("/v1/admin/nodes/%d/inbounds", n1)
	mustStatus(t, e.call(t, http.MethodPost, path1, tlsBody("cdn", "static.example.ru", "/api")), http.StatusOK)
	toNode := tlsBody("cdn2", "other.example.ru", "/api")
	toNode["tls"].(map[string]any)["decoy"] = "https://node-1.example.com"
	mustField(t, e.call(t, http.MethodPost, path1, toNode), http.StatusBadRequest, "bad_request", "tls.decoy")
	toDomain := tlsBody("cdn2", "other.example.ru", "/api")
	toDomain["tls"].(map[string]any)["decoy"] = "https://static.example.ru"
	mustField(t, e.call(t, http.MethodPost, path1, toDomain), http.StatusBadRequest, "bad_request", "tls.decoy")
	self := tlsBody("cdn2", "other.example.ru", "/api")
	self["tls"].(map[string]any)["decoy"] = "https://other.example.ru"
	mustField(t, e.call(t, http.MethodPost, path1, self), http.StatusBadRequest, "bad_request", "tls.decoy")
	site := tlsBody("cdn3", "third.example.ru", "/api")
	site["tls"].(map[string]any)["decoy"] = "https://www.example.com"
	mustStatus(t, e.call(t, http.MethodPost, path1, site), http.StatusOK)
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/nodes", map[string]any{"cert_cn": "node-3", "name": "n3", "address": "www.example.com"}), http.StatusBadRequest, "bad_request", "address")
}

func TestAdminXHTTPNeedsAnAgentThatKnowsIt(t *testing.T) {
	e := newAdminEnv(t)
	fresh := e.createNode(t, "node-fresh", nil)
	mustStatus(t, e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", fresh), tlsBody("cdn", "fresh.example.ru", "/api")), http.StatusOK)

	oldID := e.createNode(t, "node-old", nil)
	old := e.client(t, "node-old", mtls.RoleNode)
	if status := postJSON(t, old, e.ts.URL+"/v1/sync/users", map[string]any{"known_version": -1, "per_inbound": true}, nil); status != http.StatusOK {
		t.Fatalf("sync status %d", status)
	}
	oldPath := fmt.Sprintf("/v1/admin/nodes/%d/inbounds", oldID)
	mustField(t, e.call(t, http.MethodPost, oldPath, tlsBody("cdn", "old.example.ru", "/api")), http.StatusConflict, "conflict", "transport")
	mustField(t, e.call(t, http.MethodPost, oldPath, xhttpRealityBody("rx", "www.old.com")), http.StatusConflict, "conflict", "transport")
	mustStatus(t, e.call(t, http.MethodPost, oldPath, realityBody("tcp", "www.old.com")), http.StatusOK)

	if status := postJSON(t, old, e.ts.URL+"/v1/sync/users", map[string]any{"known_version": -1, "per_inbound": true, "xhttp": true}, nil); status != http.StatusOK {
		t.Fatalf("sync status %d", status)
	}
	var n struct {
		AgentXHTTP bool `json:"agent_xhttp"`
	}
	e.node(t, oldID, &n)
	if !n.AgentXHTTP {
		t.Fatalf("agent_xhttp not remembered: %+v", n)
	}
	mustStatus(t, e.call(t, http.MethodPost, oldPath, tlsBody("cdn", "old.example.ru", "/api")), http.StatusOK)
}

func TestSyncHidesXHTTPFromAgentsThatDoNotKnowIt(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-1", nil)
	path := fmt.Sprintf("/v1/admin/nodes/%d/inbounds", nodeID)
	mustStatus(t, e.call(t, http.MethodPost, path, realityBody("tcp", "www.example.com")), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodPost, path, tlsBody("cdn", "static.example.ru", "/api")), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodPost, path, xhttpRealityBody("rx", "www.other.com")), http.StatusOK)
	node := e.client(t, "node-1", mtls.RoleNode)
	tags := func(body map[string]any) []string {
		var resp struct {
			Inbounds []struct {
				Tag   string          `json:"tag"`
				XHTTP json.RawMessage `json:"xhttp"`
				TLS   json.RawMessage `json:"tls"`
			} `json:"inbounds"`
		}
		if status := postJSON(t, node, e.ts.URL+"/v1/sync/users", body, &resp); status != http.StatusOK {
			t.Fatalf("sync status %d", status)
		}
		var out []string
		for _, in := range resp.Inbounds {
			out = append(out, in.Tag)
			if in.Tag == "cdn" && (len(in.XHTTP) == 0 || len(in.TLS) == 0) {
				t.Fatalf("cdn inbound without settings: %+v", in)
			}
		}
		return out
	}
	if got := tags(map[string]any{"known_version": -1, "per_inbound": true}); fmt.Sprint(got) != "[tcp]" {
		t.Fatalf("old agent got %v", got)
	}
	if got := tags(map[string]any{"known_version": -1, "per_inbound": true, "xhttp": true}); fmt.Sprint(got) != "[cdn rx tcp]" {
		t.Fatalf("new agent got %v", got)
	}
}

func TestHeartbeatStoresCertificateStatus(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-1", nil)
	node := e.client(t, "node-1", mtls.RoleNode)
	hb := map[string]any{
		"agent_version": "1", "agent_uptime_sec": 1,
		"tls_status": map[string]any{
			"static.example.ru": map[string]any{"not_after": "2027-01-28T10:00:00Z"},
			"cdn.example.ru":    map[string]any{"error": "no certificate yet"},
		},
	}
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/heartbeat", hb, nil); status != http.StatusOK {
		t.Fatalf("heartbeat status %d", status)
	}
	var n struct {
		TLSStatus map[string]struct {
			NotAfter string `json:"not_after"`
			Error    string `json:"error"`
		} `json:"tls_status"`
	}
	e.node(t, nodeID, &n)
	if n.TLSStatus["static.example.ru"].NotAfter != "2027-01-28T10:00:00Z" || n.TLSStatus["cdn.example.ru"].Error != "no certificate yet" {
		t.Fatalf("tls_status = %+v", n)
	}
	hb["tls_status"] = "garbage"
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/heartbeat", hb, nil); status != http.StatusBadRequest {
		t.Fatalf("garbage tls_status accepted: %d", status)
	}
}

func (e *adminEnv) node(t *testing.T, id int64, v any) {
	t.Helper()
	r := e.call(t, http.MethodGet, "/v1/admin/nodes", nil)
	mustStatus(t, r, http.StatusOK)
	var list []json.RawMessage
	r.decode(t, &list)
	for _, raw := range list {
		var n struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Fatal(err)
		}
		if n.ID == id {
			if err := json.Unmarshal(raw, v); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("node %d not listed", id)
}

func TestUpgradedAgentGetsXHTTPWithoutAVersionBump(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-1", nil)
	mustStatus(t, e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", nodeID), tlsBody("cdn", "static.example.ru", "/api")), http.StatusOK)
	node := e.client(t, "node-1", mtls.RoleNode)
	var old struct {
		Version  int64             `json:"version"`
		Inbounds []json.RawMessage `json:"inbounds"`
	}
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/users", map[string]any{"known_version": -1, "per_inbound": true}, &old); status != http.StatusOK || len(old.Inbounds) != 0 {
		t.Fatalf("old agent: status %d, inbounds %d", status, len(old.Inbounds))
	}
	var upgraded struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/users", map[string]any{"known_version": old.Version, "per_inbound": true, "xhttp": true}, &upgraded); status != http.StatusOK || len(upgraded.Inbounds) != 1 {
		t.Fatalf("upgraded agent: status %d, inbounds %d", status, len(upgraded.Inbounds))
	}
	var again struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/users", map[string]any{"known_version": old.Version, "per_inbound": true, "xhttp": true}, &again); status != http.StatusOK || again.Inbounds != nil {
		t.Fatalf("same version must stay short: status %d, inbounds %v", status, again.Inbounds)
	}
}

func TestSubscriptionAPIPassesXHTTPSupport(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-1", nil)
	mustStatus(t, e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", nodeID), tlsBody("cdn", "static.example.ru", "/api")), http.StatusOK)
	u, p := botUserAndPlan(t, e.apiEnv, 8301, 199)
	if _, err := store.NewStore(e.pool).ApplyPayment(context.Background(), u.ID, p.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	subs := e.client(t, "subs", mtls.RoleSubs)
	paths := func(body map[string]any) []string {
		r := adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/subscription", body)
		mustStatus(t, r, http.StatusOK)
		var view struct {
			Connections []struct {
				Path string `json:"path"`
			} `json:"connections"`
		}
		r.decode(t, &view)
		var out []string
		for _, c := range view.Connections {
			out = append(out, c.Path)
		}
		return out
	}
	if got := paths(map[string]any{"sub_token": u.SubToken}); len(got) != 0 {
		t.Fatalf("old subs got %v", got)
	}
	if got := paths(map[string]any{"sub_token": u.SubToken, "xhttp": true}); fmt.Sprint(got) != "[/api]" {
		t.Fatalf("new subs got %v", got)
	}
}
