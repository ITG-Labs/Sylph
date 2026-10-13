package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestNULInAnyFieldIsABadRequest(t *testing.T) {
	e := newAdminEnv(t)
	if err := e.svc.UpsertNode(context.Background(), &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	subs := e.client(t, "subs", mtls.RoleSubs)
	node := e.client(t, "node-1", mtls.RoleNode)
	for _, c := range []struct {
		client *http.Client
		path   string
		body   string
	}{
		{subs, "/v1/subscription", `{"sub_token":"a\u0000b"}`},
		{subs, "/v1/subscription", `{"sub_token":"tok","user_agent":"Happ\u0000"}`},
		{node, "/v1/sync/heartbeat", `{"agent_version":"1.0\u0000","agent_uptime_sec":1}`},
		{e.panel, "/v1/admin/groups", `{"name":"x\u0000"}`},
		{e.panel, "/v1/admin/plans", `{"name":"p","price_rub":1,"period_days":30,"visibility":"pub\u0000lic"}`},
	} {
		r := adminCall(t, c.client, http.MethodPost, e.ts.URL+c.path, c.body)
		if r.status != http.StatusBadRequest {
			t.Fatalf("%s %s: status %d, want 400: %s", c.path, c.body, r.status, r.body)
		}
	}
}

func TestSyncBodiesAreLimited(t *testing.T) {
	e := newAdminEnv(t)
	if err := e.svc.UpsertNode(context.Background(), &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	node := e.client(t, "node-1", mtls.RoleNode)
	pad := strings.Repeat(" ", 70<<10)
	mustStatus(t, adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/heartbeat", `{"agent_version":"1","agent_uptime_sec":1}`+pad), http.StatusRequestEntityTooLarge)
	mustStatus(t, adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/users", `{"known_version":0}`+pad), http.StatusRequestEntityTooLarge)
	mustStatus(t, adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/heartbeat", `{"agent_version":"1","agent_uptime_sec":1,"future_field":true}`), http.StatusOK)
	var b strings.Builder
	b.WriteString(`{"batch_id":"big","deltas":[`)
	for i := range 20000 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"key_uuid":"%s","bytes_up":1,"bytes_down":1}`, model.NewUUID())
	}
	b.WriteString(`]}`)
	if b.Len() < 1<<20 {
		t.Fatalf("batch is only %d bytes", b.Len())
	}
	mustStatus(t, adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/deltas", b.String()), http.StatusOK)
	huge := `{"batch_id":"huge","deltas":[]}` + strings.Repeat(" ", 17<<20)
	mustStatus(t, adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/deltas", huge), http.StatusRequestEntityTooLarge)
}

func TestRevokedCertIsRejectedEverywhereButHealth(t *testing.T) {
	ca, err := mtls.NewCA("Sylph Test CA", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := ca.Issue("panel", mtls.ClientUsage, mtls.RolePanel, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	e := newAPIEnvWith(t, ca, mtls.NewRevocationList([]string{leaf.Cert.SerialNumber.String()}))
	c := leafClient(t, ca, leaf)
	for _, path := range []string{"/v2/anything", "/admin", "/v1/admin/overview"} {
		r := adminCall(t, c, http.MethodGet, e.ts.URL+path, nil)
		if r.status != http.StatusUnauthorized {
			t.Fatalf("revoked cert on %s: status %d", path, r.status)
		}
	}
	mustStatus(t, adminCall(t, c, http.MethodGet, e.ts.URL+"/health", nil), http.StatusOK)
}

func TestSubscriptionFailureHasItsOwnEvent(t *testing.T) {
	e := newAdminEnv(t)
	u, p := botUserAndPlan(t, e.apiEnv, 9400, 199)
	if _, err := e.svc.RecordPayment(context.Background(), "gift-1", u.ID, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `ALTER TABLE devices RENAME TO devices_gone`); err != nil {
		t.Fatal(err)
	}
	subs := e.client(t, "subs", mtls.RoleSubs)
	r := adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/subscription", map[string]any{"sub_token": u.SubToken, "hwid": "hw-1"})
	mustStatus(t, r, http.StatusInternalServerError)
	logs := e.logs.String()
	if !strings.Contains(logs, `"msg":"subscription_failed"`) || strings.Contains(logs, `"msg":"admin_failed"`) {
		t.Fatalf("logs = %s", logs)
	}
}

func TestPlanCanBeCreatedOffSale(t *testing.T) {
	e := newAdminEnv(t)
	r := e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "hidden", "price_rub": 99, "period_days": 30, "on_sale": false})
	mustStatus(t, r, http.StatusOK)
	var p struct {
		OnSale bool `json:"on_sale"`
	}
	r.decode(t, &p)
	if p.OnSale {
		t.Fatal("plan created on sale")
	}
	bot := e.client(t, "bot", mtls.RoleBot)
	r = adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/plans", nil)
	mustStatus(t, r, http.StatusOK)
	if strings.Contains(string(r.body), "hidden") {
		t.Fatalf("off-sale plan in the bot catalog: %s", r.body)
	}
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "t1", "price_rub": 0, "period_days": 3, "trial": true}), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "t2", "price_rub": 0, "period_days": 3, "trial": true, "on_sale": false}), http.StatusOK)
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "t3", "price_rub": 0, "period_days": 3, "trial": true}), http.StatusConflict, "conflict", "trial")
}

func TestInboundEdgeCasesThatAlreadyWork(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-a", nil)
	path := fmt.Sprintf("/v1/admin/nodes/%d/inbounds", nodeID)
	mustField(t, e.call(t, http.MethodPost, path, map[string]any{"tag": "main", "port": 443,
		"reality": map[string]any{"dest": "www.microsoft.com:443", "server_name": []string{"www.microsoft.com"}}}), http.StatusBadRequest, "bad_request", "")
	r := e.call(t, http.MethodPost, path, map[string]any{"tag": "main", "port": 443, "label": "one",
		"reality": map[string]any{"dest": "www.microsoft.com:443", "server_names": []string{"www.microsoft.com"}}})
	mustStatus(t, r, http.StatusOK)
	var in struct {
		ID      int64 `json:"id"`
		Reality struct {
			ShortIDs  []string `json:"short_ids"`
			PublicKey string   `json:"public_key"`
		} `json:"reality"`
	}
	r.decode(t, &in)
	mustField(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), map[string]any{"reality": map[string]any{"short_id": []string{"ab"}}}), http.StatusBadRequest, "bad_request", "")
	r = e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/inbounds/%d", in.ID), map[string]any{"label": "two"})
	mustStatus(t, r, http.StatusOK)
	var after struct {
		Label   string `json:"label"`
		Reality struct {
			ShortIDs  []string `json:"short_ids"`
			PublicKey string   `json:"public_key"`
		} `json:"reality"`
	}
	r.decode(t, &after)
	if after.Label != "two" || len(after.Reality.ShortIDs) != 1 || after.Reality.ShortIDs[0] != in.Reality.ShortIDs[0] || after.Reality.PublicKey != in.Reality.PublicKey {
		t.Fatalf("label patch changed reality: before %+v, after %+v", in, after)
	}
}

func TestInviteCarriesTheBotLink(t *testing.T) {
	e := newAdminEnv(t)
	r := e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "vip", "price_rub": 99, "period_days": 30, "visibility": "private"})
	mustStatus(t, r, http.StatusOK)
	var plan struct {
		ID int64 `json:"id"`
	}
	r.decode(t, &plan)
	invites := fmt.Sprintf("/v1/admin/plans/%d/invites", plan.ID)
	var inv struct {
		Code string  `json:"code"`
		Link *string `json:"link"`
	}
	r = e.call(t, http.MethodPost, invites, map[string]any{})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &inv)
	if inv.Link != nil {
		t.Fatalf("link without a bot name: %s", r.body)
	}
	e.svc.SetBotUsername("sylph_bot")
	r = e.call(t, http.MethodPost, invites, map[string]any{"max_uses": 1})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &inv)
	if inv.Link == nil || *inv.Link != "https://t.me/sylph_bot?start="+inv.Code {
		t.Fatalf("invite = %s", r.body)
	}
	r = e.call(t, http.MethodGet, invites, nil)
	var list []struct {
		Code string  `json:"code"`
		Link *string `json:"link"`
	}
	r.decode(t, &list)
	if len(list) != 2 || list[0].Link == nil || !strings.HasSuffix(*list[0].Link, list[0].Code) {
		t.Fatalf("list = %s", r.body)
	}
}
