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
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestUsersCreateEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleBot)
	_ = svc
	body, _ := json.Marshal(map[string]int64{"telegram_id": 700})
	resp, err := client.Post(ts.URL+"/v1/users", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m["sub_token"] == nil || m["sub_token"] == "" {
		t.Fatalf("resp = %v", m)
	}
	if _, ok := m["key_uuid"]; ok {
		t.Fatalf("key_uuid must not be returned to the bot: %v", m)
	}
	if m["id"].(float64) == 0 {
		t.Fatal("id = 0")
	}
}

func TestUsersCreateInvalidTelegram(t *testing.T) {
	ts, client, _ := newAPITestEnv(t, mtls.RoleBot)
	body, _ := json.Marshal(map[string]int64{"telegram_id": -5})
	resp, err := client.Post(ts.URL+"/v1/users", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUsersGetEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleBot)
	ctx := context.Background()
	u, _, _ := svc.RegisterUser(ctx, 800)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30, LocationGroupID: testGroup(t, svc)}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenewSubscription(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenewSubscription(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(ts.URL + "/v1/users/800")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m["subscription"] == nil || m["plan"] == nil {
		t.Fatalf("resp = %v", m)
	}
	sub := m["subscription"].(map[string]any)
	expires, _ := time.Parse(time.RFC3339Nano, sub["expires_at"].(string))
	paid, err := time.Parse(time.RFC3339Nano, fmt.Sprint(sub["paid_until"]))
	if err != nil || paid.Sub(expires) < 29*24*time.Hour {
		t.Fatalf("paid_until %v must cover the queued period after %v", sub["paid_until"], expires)
	}
}

func TestUsersGetNotFound(t *testing.T) {
	ts, client, _ := newAPITestEnv(t, mtls.RoleBot)
	resp, err := client.Get(ts.URL + "/v1/users/999999")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPaymentsEndpointIdempotent(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RolePanel)
	ctx := context.Background()
	u, _, _ := svc.RegisterUser(ctx, 900)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30, LocationGroupID: testGroup(t, svc)}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"payment_id": "pay-api-1", "user_id": u.ID, "plan_id": p.ID, "amount_rub": 100})
	for i := 0; i < 2; i++ {
		resp, err := client.Post(ts.URL+"/v1/payments", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("attempt %d: status = %d", i, resp.StatusCode)
		}
	}
}

func TestUserByTokenEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleSubs)
	ctx := context.Background()
	u, key, _ := svc.RegisterUser(ctx, 1000)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30, LocationGroupID: testGroup(t, svc)}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenewSubscription(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"sub_token": u.SubToken})
	resp, err := client.Post(ts.URL+"/v1/users/by-token", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	keys, ok := m["keys"].([]any)
	if !ok || len(keys) == 0 {
		t.Fatalf("keys = %v", m["keys"])
	}
	km, _ := keys[0].(map[string]any)
	if km["uuid"] != key {
		t.Fatalf("key uuid = %v, want %s", km["uuid"], key)
	}
}

func TestMaintenanceExpireEndpoint(t *testing.T) {
	ts, client, _ := newAPITestEnv(t, mtls.RoleCron)
	resp, err := client.Post(ts.URL+"/v1/maintenance/expire", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["expired"].(float64); !ok {
		t.Fatalf("resp = %v", m)
	}
}

func TestUserByTokenRequiresToken(t *testing.T) {
	ts, client, _ := newAPITestEnv(t, mtls.RoleSubs)
	resp, err := client.Post(ts.URL+"/v1/users/by-token", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRotateEndpointHidesKey(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleBot)
	ctx := context.Background()
	u, oldKey, _ := svc.RegisterUser(ctx, 1100)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30, LocationGroupID: testGroup(t, svc)}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenewSubscription(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(fmt.Sprintf("%s/v1/users/%d/rotate", ts.URL, u.ID), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	token, _ := m["sub_token"].(string)
	if token == "" || len(m) != 1 {
		t.Fatalf("resp = %v, want only the new sub_token", m)
	}
	_, keys, _, _, err := svc.GetUserFullByToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].UUID == oldKey {
		t.Fatalf("key not rotated: %+v", keys)
	}
}
