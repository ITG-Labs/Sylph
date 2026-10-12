package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

type adminResp struct {
	status int
	body   []byte
}

func (r adminResp) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (r adminResp) errorField(t *testing.T) (string, string) {
	t.Helper()
	var e struct {
		Code  string `json:"code"`
		Field string `json:"field"`
	}
	r.decode(t, &e)
	return e.Code, e.Field
}

func adminCall(t *testing.T, c *http.Client, method, url string, body any) adminResp {
	t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return adminResp{status: resp.StatusCode, body: raw}
}

func mustStatus(t *testing.T, r adminResp, want int) {
	t.Helper()
	if r.status != want {
		t.Fatalf("status = %d, want %d: %s", r.status, want, r.body)
	}
}

func mustField(t *testing.T, r adminResp, status int, code, field string) {
	t.Helper()
	mustStatus(t, r, status)
	gotCode, gotField := r.errorField(t)
	if gotCode != code || gotField != field {
		t.Fatalf("error = %s/%s, want %s/%s: %s", gotCode, gotField, code, field, r.body)
	}
}

type adminEnv struct {
	*apiEnv
	panel *http.Client
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	e := newAPIEnv(t)
	return &adminEnv{apiEnv: e, panel: e.client(t, "panel", mtls.RolePanel)}
}

func (e *adminEnv) call(t *testing.T, method, path string, body any) adminResp {
	t.Helper()
	return adminCall(t, e.panel, method, e.ts.URL+path, body)
}

func (e *adminEnv) createNode(t *testing.T, cn string, groupID *int64) int64 {
	t.Helper()
	r := e.call(t, http.MethodPost, "/v1/admin/nodes", map[string]any{"cert_cn": cn, "name": cn, "address": cn + ".example.com", "group_id": groupID})
	mustStatus(t, r, http.StatusOK)
	var n struct {
		ID int64 `json:"id"`
	}
	r.decode(t, &n)
	return n.ID
}

func realityBody(tag string, sni string) map[string]any {
	return map[string]any{
		"tag":   tag,
		"port":  443,
		"label": "REALITY",
		"reality": map[string]any{
			"dest":         sni + ":443",
			"server_names": []string{sni},
		},
	}
}

type inboundView struct {
	ID        int64  `json:"id"`
	NodeID    int64  `json:"node_id"`
	Tag       string `json:"tag"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	Transport string `json:"transport"`
	Security  string `json:"security"`
	Flow      string `json:"flow"`
	Label     string `json:"label"`
	Reality   struct {
		Dest        string   `json:"dest"`
		ServerNames []string `json:"server_names"`
		ShortIDs    []string `json:"short_ids"`
		Fingerprint string   `json:"fingerprint"`
		PublicKey   string   `json:"public_key"`
	} `json:"reality"`
}

func TestAdminGroups(t *testing.T) {
	e := newAdminEnv(t)
	r := e.call(t, http.MethodPost, "/v1/admin/groups", map[string]string{"name": "premium"})
	mustStatus(t, r, http.StatusOK)
	var g struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	r.decode(t, &g)
	if g.ID == 0 || g.Name != "premium" {
		t.Fatalf("group = %+v", g)
	}
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/groups", map[string]string{"name": "premium"}), http.StatusConflict, "conflict", "name")
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/groups", map[string]string{"name": ""}), http.StatusBadRequest, "bad_request", "name")
	r = e.call(t, http.MethodGet, "/v1/admin/groups", nil)
	mustStatus(t, r, http.StatusOK)
	var list []struct {
		Name string `json:"name"`
	}
	r.decode(t, &list)
	if len(list) != 1 || list[0].Name != "premium" {
		t.Fatalf("list = %+v", list)
	}
}

func TestAdminPlans(t *testing.T) {
	e := newAdminEnv(t)
	var g struct {
		ID int64 `json:"id"`
	}
	e.call(t, http.MethodPost, "/v1/admin/groups", map[string]string{"name": "premium"}).decode(t, &g)
	r := e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{
		"name": "month", "price_rub": 199, "period_days": 30,
		"traffic_limit_bytes": 100 << 30, "location_group_id": g.ID, "device_limit": 3,
	})
	mustStatus(t, r, http.StatusOK)
	var p struct {
		ID                int64  `json:"id"`
		Name              string `json:"name"`
		PriceRub          int64  `json:"price_rub"`
		TrafficLimitBytes *int64 `json:"traffic_limit_bytes"`
		LocationGroupID   *int64 `json:"location_group_id"`
		DeviceLimit       *int   `json:"device_limit"`
	}
	r.decode(t, &p)
	if p.ID == 0 || p.DeviceLimit == nil || *p.DeviceLimit != 3 || p.LocationGroupID == nil || *p.LocationGroupID != g.ID {
		t.Fatalf("plan = %+v", p)
	}
	r = e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/plans/%d", p.ID), map[string]any{"price_rub": 249, "device_limit": nil, "traffic_limit_bytes": nil})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &p)
	if p.PriceRub != 249 || p.DeviceLimit != nil || p.TrafficLimitBytes != nil || p.Name != "month" || p.LocationGroupID == nil {
		t.Fatalf("patched plan = %+v", p)
	}
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "month", "price_rub": 1, "period_days": 30}), http.StatusConflict, "conflict", "name")
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "x", "price_rub": 1, "period_days": 30, "location_group_id": 424242}), http.StatusBadRequest, "bad_request", "location_group_id")
	bad := []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"name": "", "price_rub": 1, "period_days": 30}, "name"},
		{map[string]any{"name": "a", "price_rub": -1, "period_days": 30}, "price_rub"},
		{map[string]any{"name": "a", "price_rub": 1, "period_days": 0}, "period_days"},
		{map[string]any{"name": "a", "price_rub": 1, "period_days": 30, "traffic_limit_bytes": 0}, "traffic_limit_bytes"},
		{map[string]any{"name": "a", "price_rub": 1, "period_days": 30, "device_limit": 0}, "device_limit"},
	}
	for _, b := range bad {
		mustField(t, e.call(t, http.MethodPost, "/v1/admin/plans", b.body), http.StatusBadRequest, "bad_request", b.field)
	}
	mustStatus(t, e.call(t, http.MethodPatch, "/v1/admin/plans/424242", map[string]any{"price_rub": 1}), http.StatusNotFound)
	r = e.call(t, http.MethodGet, "/v1/admin/plans", nil)
	mustStatus(t, r, http.StatusOK)
	var list []map[string]any
	r.decode(t, &list)
	if len(list) != 1 {
		t.Fatalf("plans = %v", list)
	}
}

func TestAdminNodes(t *testing.T) {
	e := newAdminEnv(t)
	var g struct {
		ID int64 `json:"id"`
	}
	e.call(t, http.MethodPost, "/v1/admin/groups", map[string]string{"name": "premium"}).decode(t, &g)
	r := e.call(t, http.MethodPost, "/v1/admin/nodes", map[string]any{"cert_cn": "node-de-1", "name": "Germany", "address": "de1.example.com", "group_id": g.ID})
	mustStatus(t, r, http.StatusOK)
	var n struct {
		ID      int64  `json:"id"`
		CertCN  string `json:"cert_cn"`
		Address string `json:"address"`
		GroupID *int64 `json:"group_id"`
		Enabled bool   `json:"enabled"`
	}
	r.decode(t, &n)
	if n.ID == 0 || !n.Enabled || n.GroupID == nil || n.Address != "de1.example.com" {
		t.Fatalf("node = %+v", n)
	}
	r = e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/nodes/%d", n.ID), map[string]any{"enabled": false, "group_id": nil, "address": "203.0.113.7"})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &n)
	if n.Enabled || n.GroupID != nil || n.Address != "203.0.113.7" || n.CertCN != "node-de-1" {
		t.Fatalf("patched node = %+v", n)
	}
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/nodes", map[string]any{"cert_cn": "node-de-1", "name": "dup", "address": "x.example.com"}), http.StatusConflict, "conflict", "cert_cn")
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/nodes", map[string]any{"cert_cn": "node-2", "name": "n2", "address": "not a host"}), http.StatusBadRequest, "bad_request", "address")
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/nodes", map[string]any{"cert_cn": "bad cn", "name": "n2", "address": "x.example.com"}), http.StatusBadRequest, "bad_request", "cert_cn")
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/nodes", map[string]any{"cert_cn": "node-3", "name": "n3", "address": "x.example.com", "group_id": 424242}), http.StatusBadRequest, "bad_request", "group_id")
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/nodes/%d", n.ID), map[string]any{"cert_cn": "renamed"}), http.StatusBadRequest, "bad_request", "")
	mustStatus(t, e.call(t, http.MethodPatch, "/v1/admin/nodes/424242", map[string]any{"name": "x"}), http.StatusNotFound)
	r = e.call(t, http.MethodGet, "/v1/admin/nodes", nil)
	mustStatus(t, r, http.StatusOK)
	var list []map[string]any
	r.decode(t, &list)
	if len(list) != 1 {
		t.Fatalf("nodes = %v", list)
	}
}

func TestAdminInboundLifecycle(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-1", nil)
	r := e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", nodeID), realityBody("reality-tcp", "www.example.com"))
	mustStatus(t, r, http.StatusOK)
	if bytes.Contains(r.body, []byte("private_key")) {
		t.Fatalf("private key exposed: %s", r.body)
	}
	var in inboundView
	r.decode(t, &in)
	if in.ID == 0 || in.NodeID != nodeID || in.Protocol != "vless" || in.Transport != "tcp" || in.Security != "reality" ||
		in.Flow != "xtls-rprx-vision" || in.Reality.Fingerprint != "chrome" || len(in.Reality.ShortIDs) != 1 || in.Reality.PublicKey == "" {
		t.Fatalf("inbound = %+v", in)
	}
	pub := in.Reality.PublicKey
	r = e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), map[string]any{"label": "Main", "reality": map[string]any{"fingerprint": "firefox"}})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &in)
	if in.Label != "Main" || in.Reality.Fingerprint != "firefox" || in.Reality.PublicKey != pub || in.Reality.Dest != "www.example.com:443" {
		t.Fatalf("patch changed more than asked: %+v", in)
	}
	r = e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), map[string]any{"reality": map[string]any{"private_key": ""}})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &in)
	if in.Reality.PublicKey == pub || in.Reality.PublicKey == "" {
		t.Fatal("empty private_key must rotate the key pair")
	}
	r = e.call(t, http.MethodGet, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", nodeID), nil)
	mustStatus(t, r, http.StatusOK)
	if bytes.Contains(r.body, []byte("private_key")) {
		t.Fatalf("private key exposed in list: %s", r.body)
	}
	var list []inboundView
	r.decode(t, &list)
	if len(list) != 1 || list[0].Label != "Main" {
		t.Fatalf("list = %+v", list)
	}
	mustStatus(t, e.call(t, http.MethodDelete, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), nil), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodDelete, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), nil), http.StatusNotFound)
	mustStatus(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), map[string]any{"label": "x"}), http.StatusNotFound)
}

func TestAdminInboundGivenKeyIsKept(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-1", nil)
	body := realityBody("reality-tcp", "www.example.com")
	priv := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	body["reality"].(map[string]any)["private_key"] = priv
	body["reality"].(map[string]any)["short_ids"] = []string{"ab12"}
	r := e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", nodeID), body)
	mustStatus(t, r, http.StatusOK)
	var in inboundView
	r.decode(t, &in)
	if in.Reality.PublicKey == "" || len(in.Reality.ShortIDs) != 1 || in.Reality.ShortIDs[0] != "ab12" {
		t.Fatalf("inbound = %+v", in)
	}
	if strings.Contains(e.logs.String(), priv) {
		t.Fatal("private key leaked into logs")
	}
}

func TestAdminInboundConflictsAndValidation(t *testing.T) {
	e := newAdminEnv(t)
	n1 := e.createNode(t, "node-1", nil)
	n2 := e.createNode(t, "node-2", nil)
	path1 := fmt.Sprintf("/v1/admin/nodes/%d/inbounds", n1)
	mustStatus(t, e.call(t, http.MethodPost, path1, realityBody("a", "www.example.com")), http.StatusOK)
	mustField(t, e.call(t, http.MethodPost, path1, realityBody("a", "www.other.com")), http.StatusConflict, "conflict", "tag")
	mustField(t, e.call(t, http.MethodPost, path1, realityBody("b", "www.example.com")), http.StatusConflict, "conflict", "reality.server_names")
	mustStatus(t, e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", n2), realityBody("a", "www.example.com")), http.StatusOK)
	r := e.call(t, http.MethodPost, path1, realityBody("b", "www.other.com"))
	mustStatus(t, r, http.StatusOK)
	var b inboundView
	r.decode(t, &b)
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", b.ID), map[string]any{"reality": map[string]any{"server_names": []string{"www.example.com"}}}), http.StatusConflict, "conflict", "reality.server_names")
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", b.ID), map[string]any{"tag": "a"}), http.StatusConflict, "conflict", "tag")
	mustStatus(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", b.ID), map[string]any{"reality": map[string]any{"server_names": []string{"www.other.com", "cdn.other.com"}}}), http.StatusOK)
	bad := realityBody("c", "www.third.com")
	bad["security"] = "tls"
	mustField(t, e.call(t, http.MethodPost, path1, bad), http.StatusBadRequest, "bad_request", "security")
	bad = realityBody("c", "www.third.com")
	bad["port"] = 8443
	mustField(t, e.call(t, http.MethodPost, path1, bad), http.StatusBadRequest, "bad_request", "port")
	bad = realityBody("c", "www.third.com")
	bad["reality"].(map[string]any)["dest"] = "nohost"
	mustField(t, e.call(t, http.MethodPost, path1, bad), http.StatusBadRequest, "bad_request", "reality.dest")
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", b.ID), map[string]any{"security": "tls"}), http.StatusBadRequest, "bad_request", "")
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/nodes/424242/inbounds", realityBody("z", "www.z.com")), http.StatusNotFound)
	mustStatus(t, e.call(t, http.MethodGet, "/v1/admin/nodes/424242/inbounds", nil), http.StatusNotFound)
}

func TestAdminRejectsBadBodies(t *testing.T) {
	e := newAdminEnv(t)
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/groups", `{"name":"a","nmae":"typo"}`), http.StatusBadRequest, "bad_request", "")
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/groups", `{"name":"a"} {"name":"b"}`), http.StatusBadRequest, "bad_request", "")
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/groups", `not json`), http.StatusBadRequest, "bad_request", "")
	huge := `{"name":"` + strings.Repeat("a", 70<<10) + `"}`
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/groups", huge), http.StatusRequestEntityTooLarge, "too_large", "")
	mustField(t, e.call(t, http.MethodPatch, "/v1/admin/nodes/abc", map[string]any{"name": "x"}), http.StatusBadRequest, "bad_request", "id")
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}

func TestAdminLegacyNodeCanBeFixedAndDisabled(t *testing.T) {
	e := newAdminEnv(t)
	legacy := &model.Node{CertCN: "legacy node", Name: "legacy"}
	if err := e.svc.UpsertNode(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/admin/nodes/%d", legacy.ID)
	r := e.call(t, http.MethodPatch, path, map[string]any{"enabled": false})
	mustStatus(t, r, http.StatusOK)
	var n struct {
		Enabled bool   `json:"enabled"`
		Address string `json:"address"`
	}
	r.decode(t, &n)
	if n.Enabled {
		t.Fatal("legacy node not disabled")
	}
	r = e.call(t, http.MethodPatch, path, map[string]any{"address": "legacy.example.com"})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &n)
	if n.Address != "legacy.example.com" {
		t.Fatalf("address = %q", n.Address)
	}
	mustField(t, e.call(t, http.MethodPatch, path, map[string]any{"address": "bad host"}), http.StatusBadRequest, "bad_request", "address")
	mustField(t, e.call(t, http.MethodPatch, path, map[string]any{"name": ""}), http.StatusBadRequest, "bad_request", "name")
}

func TestAdminPlanHWIDTrialVisibility(t *testing.T) {
	e := newAdminEnv(t)
	type plan struct {
		ID           int64  `json:"id"`
		DeviceLimit  *int   `json:"device_limit"`
		HWIDRequired bool   `json:"hwid_required"`
		Trial        bool   `json:"trial"`
		Visibility   string `json:"visibility"`
	}
	var limited, open, trial plan
	r := e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "limited", "price_rub": 1, "period_days": 30, "device_limit": 2})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &limited)
	if !limited.HWIDRequired || limited.Trial || limited.Visibility != "public" {
		t.Fatalf("limited = %+v", limited)
	}
	r = e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "open", "price_rub": 1, "period_days": 30})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &open)
	if open.HWIDRequired {
		t.Fatalf("open = %+v", open)
	}
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "hwid-only", "price_rub": 1, "period_days": 30, "hwid_required": true}), http.StatusOK)
	r = e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "trial", "price_rub": 0, "period_days": 3, "trial": true, "visibility": "private"})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &trial)
	if !trial.Trial || trial.Visibility != "private" {
		t.Fatalf("trial = %+v", trial)
	}
	bad := []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"name": "a", "price_rub": 1, "period_days": 30, "device_limit": 2, "hwid_required": false}, "hwid_required"},
		{map[string]any{"name": "a", "price_rub": 5, "period_days": 3, "trial": true}, "price_rub"},
		{map[string]any{"name": "a", "price_rub": 1, "period_days": 30, "visibility": "secret"}, "visibility"},
	}
	for _, b := range bad {
		mustField(t, e.call(t, http.MethodPost, "/v1/admin/plans", b.body), http.StatusBadRequest, "bad_request", b.field)
	}
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "trial-2", "price_rub": 0, "period_days": 3, "trial": true}), http.StatusConflict, "conflict", "trial")
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/plans/%d", limited.ID), map[string]any{"hwid_required": false}), http.StatusBadRequest, "bad_request", "hwid_required")
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/plans/%d", open.ID), map[string]any{"device_limit": 3}), http.StatusBadRequest, "bad_request", "hwid_required")
	r = e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/plans/%d", limited.ID), map[string]any{"device_limit": nil, "hwid_required": false, "visibility": "private"})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &limited)
	if limited.HWIDRequired || limited.DeviceLimit != nil || limited.Visibility != "private" {
		t.Fatalf("patched limited = %+v", limited)
	}
	mustStatus(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/plans/%d", trial.ID), map[string]any{"on_sale": false}), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "trial-2", "price_rub": 0, "period_days": 3, "trial": true}), http.StatusOK)
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/plans/%d", trial.ID), map[string]any{"on_sale": true}), http.StatusConflict, "conflict", "trial")
}
