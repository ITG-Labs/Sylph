package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func profileDoc(extra ...map[string]any) map[string]any {
	outbounds := []any{
		map[string]any{"tag": "direct", "kind": "direct"},
		map[string]any{"tag": "block", "kind": "block"},
	}
	for _, e := range extra {
		outbounds = append(outbounds, e)
	}
	return map[string]any{
		"outbounds": outbounds,
		"rules":     []any{map[string]any{"protocol": []string{"bittorrent"}, "to": "block"}},
		"default":   "direct",
	}
}

type profileResp struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	Revision int64          `json:"revision"`
	Doc      map[string]any `json:"doc"`
	NodeIDs  []int64        `json:"node_ids"`
}

func TestAdminProfileLifecycle(t *testing.T) {
	e := newAdminEnv(t)
	trojan := map[string]any{"tag": "fi", "kind": "proxy", "proxy": map[string]any{"protocol": "trojan", "address": "fi.example.com", "port": 443, "password": "hunter2"}}
	r := e.call(t, http.MethodPost, "/v1/admin/profiles", map[string]any{"name": "Базовый", "doc": profileDoc(trojan)})
	mustStatus(t, r, http.StatusOK)
	if strings.Contains(string(r.body), "hunter2") {
		t.Fatalf("secret leaked: %s", r.body)
	}
	var p profileResp
	r.decode(t, &p)
	if p.ID == 0 || p.Revision != 1 || p.Name != "Базовый" {
		t.Fatalf("created = %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/profiles", map[string]any{"name": "x", "doc": map[string]any{"default": "nope"}}), http.StatusBadRequest, "bad_request", "doc.outbounds")
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/profiles", map[string]any{"name": "Базовый", "doc": profileDoc()}), http.StatusConflict, "conflict", "name")
	path := fmt.Sprintf("/v1/admin/profiles/%d", p.ID)
	r = e.call(t, http.MethodPut, path, map[string]any{"name": "Базовый", "doc": profileDoc(trojan), "revision": 1})
	mustStatus(t, r, http.StatusOK)
	mustField(t, e.call(t, http.MethodPut, path, map[string]any{"name": "Базовый", "doc": profileDoc(), "revision": 1}), http.StatusConflict, "conflict", "revision")
	mustStatus(t, e.call(t, http.MethodPut, "/v1/admin/profiles/999", map[string]any{"name": "x", "doc": profileDoc(), "revision": 1}), http.StatusNotFound)

	nodeID := e.createNode(t, "node-p", nil)
	mustStatus(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/nodes/%d", nodeID), map[string]any{"profile_id": p.ID}), http.StatusOK)
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/nodes/%d", nodeID), map[string]any{"profile_id": 999}), http.StatusBadRequest, "bad_request", "profile_id")
	r = e.call(t, http.MethodGet, path, nil)
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &p)
	if len(p.NodeIDs) != 1 || p.NodeIDs[0] != nodeID || p.Revision != 2 {
		t.Fatalf("profile = %s", r.body)
	}
	r = e.call(t, http.MethodGet, fmt.Sprintf("%s/preview?node_id=%d", path, nodeID), nil)
	mustStatus(t, r, http.StatusOK)
	if !strings.Contains(string(r.body), `"***"`) || strings.Contains(string(r.body), "hunter2") || !strings.Contains(string(r.body), "trojan") {
		t.Fatalf("preview = %s", r.body)
	}
	mustStatus(t, e.call(t, http.MethodGet, path+"/preview?node_id=999", nil), http.StatusNotFound)
	mustStatus(t, e.call(t, http.MethodGet, path+"/preview?node=1", nil), http.StatusBadRequest)
	r = e.call(t, http.MethodGet, "/v1/admin/profiles", nil)
	mustStatus(t, r, http.StatusOK)
	if !strings.Contains(string(r.body), `"nodes":1`) {
		t.Fatalf("list = %s", r.body)
	}
	mustField(t, e.call(t, http.MethodDelete, path, nil), http.StatusConflict, "conflict", "profile")
	mustStatus(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/nodes/%d", nodeID), map[string]any{"profile_id": nil}), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodDelete, path, nil), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodGet, path, nil), http.StatusNotFound)

	for _, a := range auditList(t, e, "?entity=profile") {
		if strings.Contains(fmt.Sprint(a.Details), "hunter2") {
			t.Fatalf("audit leaks the secret: %+v", a.Details)
		}
	}
	if got := auditList(t, e, "?entity=profile"); len(got) != 3 {
		t.Fatalf("audit = %+v", got)
	}
}

func TestAdminGeoSources(t *testing.T) {
	e := newAdminEnv(t)
	r := e.call(t, http.MethodPost, "/v1/admin/geo-sources", map[string]any{"name": "ru-blocked", "kind": "domain", "url": "https://example.com/geosite.dat"})
	mustStatus(t, r, http.StatusOK)
	var g struct {
		ID   int64  `json:"id"`
		URL  string `json:"url"`
		Kind string `json:"kind"`
	}
	r.decode(t, &g)
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/geo-sources", map[string]any{"name": "x", "kind": "ip", "url": "http://example.com/a.dat"}), http.StatusBadRequest, "bad_request", "url")
	path := fmt.Sprintf("/v1/admin/geo-sources/%d", g.ID)
	r = e.call(t, http.MethodPatch, path, map[string]any{"url": "https://mirror.example.com/geosite.dat"})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &g)
	if g.URL != "https://mirror.example.com/geosite.dat" || g.Kind != "domain" {
		t.Fatalf("patched = %s", r.body)
	}
	r = e.call(t, http.MethodGet, "/v1/admin/geo-sources", nil)
	mustStatus(t, r, http.StatusOK)
	if !strings.Contains(string(r.body), "ru-blocked") {
		t.Fatalf("list = %s", r.body)
	}
	r = e.call(t, http.MethodPost, "/v1/admin/geo-sources/refresh", nil)
	mustStatus(t, r, http.StatusOK)
	if !strings.Contains(string(r.body), `"generation":1`) {
		t.Fatalf("refresh = %s", r.body)
	}
	mustStatus(t, e.call(t, http.MethodDelete, path, nil), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodDelete, path, nil), http.StatusNotFound)
	if got := auditList(t, e, "?entity=geo_source"); len(got) != 4 {
		t.Fatalf("audit = %+v", got)
	}
}

func TestHeartbeatKeepsGeoStatus(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-hb", nil)
	node := e.client(t, "node-hb", mtls.RoleNode)
	beat := func(body string) {
		t.Helper()
		mustStatus(t, adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/heartbeat", body), http.StatusOK)
	}
	beat(`{"agent_version":"1","agent_uptime_sec":5,"geo_status":{"ru-blocked":{"sha256":"ab","size":10}},"routing_error":"AddRule failed"}`)
	r := e.call(t, http.MethodGet, "/v1/admin/nodes", nil)
	mustStatus(t, r, http.StatusOK)
	if !strings.Contains(string(r.body), `"routing_error":"AddRule failed"`) || !strings.Contains(string(r.body), `"sha256":"ab"`) || !strings.Contains(string(r.body), `"size":10`) {
		t.Fatalf("nodes = %s", r.body)
	}
	beat(`{"agent_version":"1","agent_uptime_sec":5,"geo_status":["not","an","object"]}`)
	r = e.call(t, http.MethodGet, "/v1/admin/nodes", nil)
	if !strings.Contains(string(r.body), `"geo_status":null`) || !strings.Contains(string(r.body), `"routing_error":null`) {
		t.Fatalf("bad geo status kept: %s", r.body)
	}
	_ = nodeID
}
