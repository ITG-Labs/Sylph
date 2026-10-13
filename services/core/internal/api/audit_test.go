package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

type auditRow struct {
	PeerCN   string         `json:"peer_cn"`
	Operator string         `json:"operator"`
	Method   string         `json:"method"`
	Route    string         `json:"route"`
	Path     string         `json:"path"`
	Entity   string         `json:"entity"`
	EntityID string         `json:"entity_id"`
	Action   string         `json:"action"`
	Details  map[string]any `json:"details"`
}

func operatorCall(t *testing.T, c *http.Client, method, url, operator string, body string) adminResp {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if operator != "" {
		req.Header.Set("X-Sylph-Operator", operator)
	}
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

func auditList(t *testing.T, e *adminEnv, query string) []auditRow {
	t.Helper()
	r := e.call(t, http.MethodGet, "/v1/admin/audit"+query, nil)
	mustStatus(t, r, http.StatusOK)
	var page struct {
		Entries []auditRow `json:"entries"`
	}
	r.decode(t, &page)
	return page.Entries
}

func TestAuditRecordsSuccessfulPanelChanges(t *testing.T) {
	e := newAdminEnv(t)
	r := operatorCall(t, e.panel, http.MethodPost, e.ts.URL+"/v1/admin/groups", "Иван", `{"name":"premium"}`)
	mustStatus(t, r, http.StatusOK)
	mustStatus(t, operatorCall(t, e.panel, http.MethodPost, e.ts.URL+"/v1/admin/groups", "Иван", `{"name":""}`), http.StatusBadRequest)
	e.call(t, http.MethodGet, "/v1/admin/groups", nil)
	nodeID := e.createNode(t, "node-a", nil)
	r = e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", nodeID), map[string]any{
		"tag": "main", "port": 443,
		"reality": map[string]any{"dest": "www.microsoft.com:443", "server_names": []string{"www.microsoft.com"},
			"private_key": "4Ij2vUEnBb1ABGs-JR2yMyD7pYvF6Bm1CntlHdUfu1g"},
	})
	mustStatus(t, r, http.StatusOK)
	bot := e.client(t, "bot", mtls.RoleBot)
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users", map[string]any{"telegram_id": 42}), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodPost, "/v1/users/by-token", map[string]any{"sub_token": "secret-token"}), http.StatusNotFound)

	all := auditList(t, e, "")
	if len(all) != 3 {
		t.Fatalf("audit = %+v", all)
	}
	group := all[2]
	if group.Entity != "group" || group.Action != "create" || group.EntityID == "" || group.Operator != "Иван" || group.PeerCN != "panel" ||
		group.Method != "POST" || group.Route != "POST /v1/admin/groups" || group.Path != "/v1/admin/groups" || group.Details["name"] != "premium" {
		t.Fatalf("group entry = %+v", group)
	}
	in := all[0]
	reality, _ := in.Details["reality"].(map[string]any)
	if in.Entity != "inbound" || in.Action != "create" || reality == nil || reality["private_key"] != "***" || reality["dest"] != "www.microsoft.com:443" {
		t.Fatalf("inbound entry = %+v", in)
	}
	if strings.Contains(e.logs.String(), "4Ij2vUEnBb1ABGs") {
		t.Fatal("private key leaked into the logs")
	}
	if got := auditList(t, e, "?entity=group"); len(got) != 1 || got[0].Entity != "group" {
		t.Fatalf("filtered = %+v", got)
	}
	if got := auditList(t, e, fmt.Sprintf("?entity=node&entity_id=%d", nodeID)); len(got) != 1 {
		t.Fatalf("node entries = %+v", got)
	}
	if !strings.Contains(e.logs.String(), `"operator":"Иван"`) {
		t.Fatal("admin_change log has no operator")
	}
}

func TestAuditKeepsSecretsOutOfServiceCalls(t *testing.T) {
	e := newAdminEnv(t)
	u, _ := botUserAndPlan(t, e.apiEnv, 9300, 199)
	mustStatus(t, e.call(t, http.MethodPost, "/v1/users/by-token", map[string]any{"sub_token": u.SubToken}), http.StatusOK)
	all := auditList(t, e, "")
	if len(all) != 1 || all[0].Details["sub_token"] != "***" || all[0].Route != "POST /v1/users/by-token" {
		t.Fatalf("audit = %+v", all)
	}
}

func TestBadOperatorHeaderIsRejected(t *testing.T) {
	e := newAdminEnv(t)
	r := operatorCall(t, e.panel, http.MethodPost, e.ts.URL+"/v1/admin/groups", strings.Repeat("я", 65), `{"name":"g"}`)
	mustField(t, r, http.StatusBadRequest, "bad_request", "X-Sylph-Operator")
	if got := auditList(t, e, ""); len(got) != 0 {
		t.Fatalf("rejected request audited: %+v", got)
	}
	mustField(t, e.call(t, http.MethodGet, "/v1/admin/audit?limit=0", nil), http.StatusBadRequest, "bad_request", "limit")
}

func TestTwoOperatorHeadersAreRejected(t *testing.T) {
	e := newAdminEnv(t)
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+"/v1/admin/groups", strings.NewReader(`{"name":"g"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Add("X-Sylph-Operator", "alice")
	req.Header.Add("X-Sylph-Operator", "bob")
	resp, err := e.panel.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
