package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func postJSON(t *testing.T, c *http.Client, url string, in any, out any) int {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func userKeysByToken(t *testing.T, c *http.Client, base string, token string) []string {
	t.Helper()
	var full struct {
		Keys []struct {
			UUID string `json:"uuid"`
		} `json:"keys"`
	}
	if status := postJSON(t, c, base+"/v1/users/by-token", map[string]string{"sub_token": token}, &full); status != http.StatusOK {
		t.Fatalf("by-token status = %d", status)
	}
	var out []string
	for _, k := range full.Keys {
		out = append(out, k.UUID)
	}
	return out
}

func TestNoSecretsInLogs(t *testing.T) {
	e := newAPIEnv(t)
	ctx := context.Background()
	bot := e.client(t, "bot", mtls.RoleBot)
	subs := e.client(t, "subs", mtls.RoleSubs)
	cron := e.client(t, "cron", mtls.RoleCron)
	panel := e.client(t, "panel", mtls.RolePanel)
	node := e.client(t, "node-1", mtls.RoleNode)
	nodeRow := &model.Node{CertCN: "node-1", Name: "n1", Address: "n1.example.com", Enabled: true}
	if err := e.svc.CreateNode(ctx, nodeRow); err != nil {
		t.Fatal(err)
	}
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := e.svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}

	var created struct {
		ID       int64  `json:"id"`
		SubToken string `json:"sub_token"`
	}
	if status := postJSON(t, bot, e.ts.URL+"/v1/users", map[string]int64{"telegram_id": 4242}, &created); status != http.StatusOK {
		t.Fatalf("create status = %d", status)
	}
	if status := postJSON(t, bot, e.ts.URL+"/v1/users", map[string]int64{"telegram_id": 4242}, nil); status != http.StatusInternalServerError {
		t.Fatalf("duplicate create status = %d, want 500", status)
	}
	if status := postJSON(t, panel, e.ts.URL+"/v1/payments", map[string]any{"payment_id": "pay-secret-test", "user_id": created.ID, "plan_id": p.ID, "amount_rub": 100}, nil); status != http.StatusOK {
		t.Fatalf("payment status = %d", status)
	}
	resp, err := bot.Get(e.ts.URL + "/v1/users/4242")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	secrets := []string{created.SubToken, "wrong-token-that-must-not-leak"}
	keys := userKeysByToken(t, subs, e.ts.URL, created.SubToken)
	secrets = append(secrets, keys...)
	if status := postJSON(t, subs, e.ts.URL+"/v1/users/by-token", map[string]string{"sub_token": "wrong-token-that-must-not-leak"}, nil); status != http.StatusNotFound {
		t.Fatalf("wrong token status = %d, want 404", status)
	}
	privateKey := "kBhuJbKHFP3JcX9AD-CjqMh7S83kM_fuhJJ1xLhkY1M"
	var inb struct {
		ID int64 `json:"id"`
	}
	inbBody := map[string]any{"tag": "main", "port": 443, "reality": map[string]any{"dest": "www.example.com:443", "server_names": []string{"www.example.com"}, "private_key": privateKey}}
	if status := postJSON(t, panel, fmt.Sprintf("%s/v1/admin/nodes/%d/inbounds", e.ts.URL, nodeRow.ID), inbBody, &inb); status != http.StatusOK {
		t.Fatalf("inbound status = %d", status)
	}
	secrets = append(secrets, privateKey, "hwid-must-not-leak")
	if status := postJSON(t, subs, e.ts.URL+"/v1/subscription", map[string]string{"sub_token": created.SubToken, "hwid": "hwid-must-not-leak", "user_agent": "Happ/1.0"}, nil); status != http.StatusOK {
		t.Fatalf("subscription status = %d", status)
	}
	if status := postJSON(t, subs, e.ts.URL+"/v1/subscription", map[string]string{"sub_token": "wrong-token-that-must-not-leak"}, nil); status != http.StatusNotFound {
		t.Fatalf("subscription wrong token status = %d, want 404", status)
	}
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/users", map[string]int64{"known_version": -1}, nil); status != http.StatusOK {
		t.Fatalf("sync status = %d", status)
	}
	deltas := map[string]any{"batch_id": "b-secret-test", "deltas": []map[string]any{{"key_uuid": keys[0], "bytes_up": 10, "bytes_down": 20}}}
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/deltas", deltas, nil); status != http.StatusOK {
		t.Fatalf("deltas status = %d", status)
	}
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/heartbeat", map[string]any{"agent_version": "v1", "xray_version": "25.9.11", "agent_uptime_sec": 1, "xray_uptime_sec": 1, "active_users": 1}, nil); status != http.StatusOK {
		t.Fatalf("heartbeat status = %d", status)
	}
	var rotated map[string]any
	if status := postJSON(t, bot, fmt.Sprintf("%s/v1/users/%d/rotate", e.ts.URL, created.ID), nil, &rotated); status != http.StatusOK {
		t.Fatalf("rotate status = %d", status)
	}
	secrets = append(secrets, userKeysByToken(t, subs, e.ts.URL, created.SubToken)...)
	if status := postJSON(t, bot, fmt.Sprintf("%s/v1/users/%d/devices/reset", e.ts.URL, created.ID), nil, nil); status != http.StatusOK {
		t.Fatalf("reset status = %d", status)
	}
	if status := postJSON(t, cron, e.ts.URL+"/v1/maintenance/expire", nil, nil); status != http.StatusOK {
		t.Fatalf("expire status = %d", status)
	}
	if status := postJSON(t, node, e.ts.URL+"/v1/users/by-token", map[string]string{"sub_token": created.SubToken}, nil); status != http.StatusForbidden {
		t.Fatalf("node by-token status = %d, want 403", status)
	}

	logs := e.logs.String()
	if !strings.Contains(logs, `"msg":"http_request"`) || !strings.Contains(logs, `"msg":"user_create_failed"`) {
		t.Fatalf("scenario did not log as expected: %s", logs)
	}
	for _, s := range secrets {
		if s == "" {
			t.Fatal("empty secret in scenario")
		}
		if strings.Contains(logs, s) {
			t.Errorf("secret %q leaked into logs", s)
		}
	}
}
