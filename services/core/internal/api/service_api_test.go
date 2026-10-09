package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestUsersCreateEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t)
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
	if m["sub_token"] == "" || m["key_uuid"] == "" {
		t.Fatalf("resp = %v", m)
	}
	if m["id"].(float64) == 0 {
		t.Fatal("id = 0")
	}
}

func TestUsersCreateInvalidTelegram(t *testing.T) {
	ts, client, _ := newAPITestEnv(t)
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
	ts, client, svc := newAPITestEnv(t)
	ctx := context.Background()
	u, _, _ := svc.RegisterUser(ctx, 800)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := svc.CreatePlanTest(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenewSubscriptionTest(ctx, u.ID, p.ID); err != nil {
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
}

func TestUsersGetNotFound(t *testing.T) {
	ts, client, _ := newAPITestEnv(t)
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
	ts, client, svc := newAPITestEnv(t)
	ctx := context.Background()
	u, _, _ := svc.RegisterUser(ctx, 900)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := svc.CreatePlanTest(ctx, p); err != nil {
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
	ts, client, svc := newAPITestEnv(t)
	ctx := context.Background()
	u, key, _ := svc.RegisterUser(ctx, 1000)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := svc.CreatePlanTest(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenewSubscriptionTest(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(ts.URL + "/v1/users/by-token/" + u.SubToken)
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
	ts, client, _ := newAPITestEnv(t)
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
