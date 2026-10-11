package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestSubscriptionEndpoint(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	subs := e.client(t, "subs", mtls.RoleSubs)
	nodeID := e.createNode(t, "node-1", nil)
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/nodes/"+itoa(nodeID)+"/inbounds", realityBody("main", "www.example.com")), http.StatusOK)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := e.svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	u, key, err := e.svc.RegisterUser(ctx, 4100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RenewSubscription(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	r := adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/subscription", map[string]string{
		"sub_token": u.SubToken, "hwid": "hw-1", "user_agent": "Happ/1.6.0", "device_os": "Android", "device_model": "Pixel 8",
	})
	mustStatus(t, r, http.StatusOK)
	var v struct {
		Status            string     `json:"status"`
		TrafficUsedBytes  int64      `json:"traffic_used_bytes"`
		TrafficLimitBytes *int64     `json:"traffic_limit_bytes"`
		ExpiresAt         *time.Time `json:"expires_at"`
		Connections       []struct {
			Name      string `json:"name"`
			Address   string `json:"address"`
			Port      int    `json:"port"`
			UUID      string `json:"uuid"`
			Flow      string `json:"flow"`
			SNI       string `json:"sni"`
			PublicKey string `json:"public_key"`
			ShortID   string `json:"short_id"`
		} `json:"connections"`
	}
	r.decode(t, &v)
	if v.Status != "ok" || v.ExpiresAt == nil || v.TrafficLimitBytes != nil || len(v.Connections) != 1 {
		t.Fatalf("view = %+v", v)
	}
	c := v.Connections[0]
	if c.Name != "node-1" || c.Address != "node-1.example.com" || c.Port != 443 || c.UUID != key ||
		c.Flow != "xtls-rprx-vision" || c.SNI != "www.example.com" || c.PublicKey == "" || c.ShortID == "" {
		t.Fatalf("connection = %+v", c)
	}
	if strings.Contains(string(r.body), "private_key") {
		t.Fatalf("private key in subscription: %s", r.body)
	}
	mustStatus(t, adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/subscription", map[string]string{"sub_token": "0123456789abcdef0123456789abcdef"}), http.StatusNotFound)
	mustField(t, adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/subscription", map[string]string{}), http.StatusBadRequest, "bad_request", "sub_token")
	mustField(t, adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/subscription", map[string]string{"sub_token": u.SubToken, "hwid": strings.Repeat("h", 257)}), http.StatusBadRequest, "bad_request", "hwid")
	mustStatus(t, adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/subscription", map[string]string{"sub_token": u.SubToken, "token": "typo"}), http.StatusBadRequest)
	mustStatus(t, adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/devices", map[string]string{"sub_token": u.SubToken}), http.StatusNotFound)
}

func TestSubscriptionLogsWithoutSecrets(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	subs := e.client(t, "subs", mtls.RoleSubs)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := e.svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	u, _, err := e.svc.RegisterUser(ctx, 4200)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RenewSubscription(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	mustStatus(t, adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/subscription", map[string]string{"sub_token": u.SubToken, "hwid": "hwid-secret-value", "user_agent": "v2rayNG/1.9.30"}), http.StatusOK)
	logs := e.logs.String()
	if !strings.Contains(logs, `"msg":"subscription_view"`) || !strings.Contains(logs, `"client":"v2rayNG"`) || !strings.Contains(logs, `"status":"ok"`) {
		t.Fatalf("subscription_view not logged as expected: %s", logs)
	}
	for _, secret := range []string{u.SubToken, "hwid-secret-value"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("secret %q leaked into logs", secret)
		}
	}
}
