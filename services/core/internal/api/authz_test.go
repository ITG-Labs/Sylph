package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

type routeCase struct {
	method  string
	path    string
	allowed []string
}

var routeMatrix = []routeCase{
	{http.MethodPost, "/v1/users", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodGet, "/v1/users/1", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodPost, "/v1/users/by-token", []string{mtls.RoleSubs, mtls.RolePanel}},
	{http.MethodPost, "/v1/payments", []string{mtls.RolePanel}},
	{http.MethodPost, "/v1/payments/create", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodPost, "/v1/payments/2d85d5ad-000f-5000-9000-1c4c6dd1d9e6/check", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodGet, "/v1/plans", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodPost, "/v1/maintenance/payments/poll", []string{mtls.RoleCron, mtls.RolePanel}},
	{http.MethodPost, "/v1/users/1/rotate", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodPost, "/v1/users/1/devices/reset", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodGet, "/v1/users/1/devices", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodDelete, "/v1/users/1/devices/1", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodPost, "/v1/users/1/trial", []string{mtls.RoleBot, mtls.RolePanel}},
	{http.MethodPost, "/v1/subscription", []string{mtls.RoleSubs, mtls.RolePanel}},
	{http.MethodPost, "/v1/maintenance/expire", []string{mtls.RoleCron, mtls.RolePanel}},
	{http.MethodPost, "/v1/maintenance/cleanup", []string{mtls.RoleCron, mtls.RolePanel}},
	{http.MethodPost, "/v1/sync/users", []string{mtls.RoleNode}},
	{http.MethodPost, "/v1/sync/deltas", []string{mtls.RoleNode}},
	{http.MethodPost, "/v1/sync/heartbeat", []string{mtls.RoleNode}},
	{http.MethodPost, "/v1/admin/groups", []string{mtls.RolePanel}},
	{http.MethodGet, "/v1/admin/groups", []string{mtls.RolePanel}},
	{http.MethodPost, "/v1/admin/plans", []string{mtls.RolePanel}},
	{http.MethodGet, "/v1/admin/plans", []string{mtls.RolePanel}},
	{http.MethodPatch, "/v1/admin/plans/1", []string{mtls.RolePanel}},
	{http.MethodPost, "/v1/admin/nodes", []string{mtls.RolePanel}},
	{http.MethodGet, "/v1/admin/nodes", []string{mtls.RolePanel}},
	{http.MethodPatch, "/v1/admin/nodes/1", []string{mtls.RolePanel}},
	{http.MethodGet, "/v1/admin/nodes/1/inbounds", []string{mtls.RolePanel}},
	{http.MethodPost, "/v1/admin/nodes/1/inbounds", []string{mtls.RolePanel}},
	{http.MethodPatch, "/v1/admin/inbounds/1", []string{mtls.RolePanel}},
	{http.MethodDelete, "/v1/admin/inbounds/1", []string{mtls.RolePanel}},
}

type caller struct {
	name   string
	role   string
	client *http.Client
}

func matrixCallers(t *testing.T, e *apiEnv) []caller {
	t.Helper()
	return []caller{
		{"bot", mtls.RoleBot, e.client(t, "bot", mtls.RoleBot)},
		{"subs", mtls.RoleSubs, e.client(t, "subs", mtls.RoleSubs)},
		{"cron", mtls.RoleCron, e.client(t, "cron", mtls.RoleCron)},
		{"panel", mtls.RolePanel, e.client(t, "panel", mtls.RolePanel)},
		{"node", mtls.RoleNode, e.client(t, "node-1", mtls.RoleNode)},
		{"no-ou", "", e.clientWithOU(t, "bot", nil)},
		{"two-ou", "", e.clientWithOU(t, "bot", []string{mtls.RoleBot, mtls.RolePanel})},
		{"unknown-ou", "", e.clientWithOU(t, "bot", []string{"admin"})},
	}
}

func callRoute(t *testing.T, c *http.Client, base string, rc routeCase) (int, string) {
	t.Helper()
	var body *bytes.Reader
	if rc.method == http.MethodPost || rc.method == http.MethodPatch {
		body = bytes.NewReader([]byte(`{}`))
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(rc.method, base+rc.path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return resp.StatusCode, payload.Code
}

func TestRouteMatrix(t *testing.T) {
	e := newAPIEnv(t)
	if err := e.svc.UpsertNode(context.Background(), &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range matrixCallers(t, e) {
		for _, rc := range routeMatrix {
			status, code := callRoute(t, c.client, e.ts.URL, rc)
			denied := status == http.StatusForbidden && code == "forbidden"
			want := c.role != "" && slices.Contains(rc.allowed, c.role)
			if want && denied {
				t.Errorf("%s %s as %s: denied, want allowed", rc.method, rc.path, c.name)
			}
			if !want && !denied {
				t.Errorf("%s %s as %s: status %d code %q, want 403 forbidden", rc.method, rc.path, c.name, status, code)
			}
		}
	}
}

func TestAuthzDeniedIsLogged(t *testing.T) {
	e := newAPIEnv(t)
	node := e.client(t, "node-7", mtls.RoleNode)
	status, code := callRoute(t, node, e.ts.URL, routeCase{method: http.MethodPost, path: "/v1/payments"})
	if status != http.StatusForbidden || code != "forbidden" {
		t.Fatalf("status %d code %q, want 403 forbidden", status, code)
	}
	var found bool
	for _, line := range strings.Split(e.logs.String(), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		if m["msg"] == "authz_denied" {
			found = true
			if m["peer"] != "node-7" || m["role"] != "node" || m["path"] != "/v1/payments" || m["method"] != "POST" {
				t.Fatalf("authz_denied fields = %v", m)
			}
		}
	}
	if !found {
		t.Fatalf("no authz_denied in logs: %s", e.logs.String())
	}
}

func TestRouteWithoutRolesPanics(t *testing.T) {
	s := NewServer(nil, testLogger(), mtls.NewRevocationList(nil))
	defer func() {
		if recover() == nil {
			t.Fatal("route without roles must panic")
		}
	}()
	s.route(http.NewServeMux(), "POST /v1/x", func(w http.ResponseWriter, r *http.Request) {})
}

func TestRouteWithUnknownRolePanics(t *testing.T) {
	s := NewServer(nil, testLogger(), mtls.NewRevocationList(nil))
	defer func() {
		if recover() == nil {
			t.Fatal("route with unknown role must panic")
		}
	}()
	s.route(http.NewServeMux(), "POST /v1/x", func(w http.ResponseWriter, r *http.Request) {}, "admin")
}

func TestRoleBeatsCNNaming(t *testing.T) {
	e := newAPIEnv(t)
	ctx := context.Background()
	if err := e.svc.UpsertNode(ctx, &model.Node{CertCN: "bot", Name: "misnamed"}); err != nil {
		t.Fatal(err)
	}
	bot := e.client(t, "bot", mtls.RoleBot)
	if status, code := callRoute(t, bot, e.ts.URL, routeCase{method: http.MethodPost, path: "/v1/sync/users"}); status != http.StatusForbidden || code != "forbidden" {
		t.Fatalf("bot cert on sync with a node named bot: status %d code %q, want 403 forbidden", status, code)
	}
	nodeNamedPanel := e.client(t, "panel", mtls.RoleNode)
	if status, code := callRoute(t, nodeNamedPanel, e.ts.URL, routeCase{method: http.MethodPost, path: "/v1/payments"}); status != http.StatusForbidden || code != "forbidden" {
		t.Fatalf("node cert named panel on payments: status %d code %q, want 403 forbidden", status, code)
	}
}

func TestRevokedCertRejectedBeforeRole(t *testing.T) {
	ca, err := mtls.NewCA("Sylph Test CA", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := ca.Issue("panel", mtls.ClientUsage, mtls.RolePanel, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	e := newAPIEnvWith(t, ca, mtls.NewRevocationList([]string{leaf.Cert.SerialNumber.String()}))
	status, _ := callRoute(t, leafClient(t, ca, leaf), e.ts.URL, routeCase{method: http.MethodPost, path: "/v1/maintenance/expire"})
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked panel cert: status %d, want 401", status)
	}
}
