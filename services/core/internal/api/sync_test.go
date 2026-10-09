package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestSyncUsersEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := svc.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	u, key, _ := svc.RegisterUser(ctx, 600)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenewSubscription(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]int64{"known_version": 0})
	resp, err := client.Post(ts.URL+"/v1/sync/users", "application/json", bytes.NewReader(body))
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
	users, ok := m["users"].([]any)
	if !ok || len(users) == 0 {
		t.Fatalf("users = %v", m["users"])
	}
	found := false
	for _, u := range users {
		um, _ := u.(map[string]any)
		if um["uuid"] == key {
			found = true
		}
	}
	if !found {
		t.Fatalf("key %s not in sync response", key)
	}
}

func TestSyncUsersUnknownNode(t *testing.T) {
	ts, client, _ := newAPITestEnv(t)
	body, _ := json.Marshal(map[string]int64{"known_version": 0})
	resp, err := client.Post(ts.URL+"/v1/sync/users", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestSyncDeltasEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := svc.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	u, key, _ := svc.RegisterUser(ctx, 601)
	_ = u
	body, _ := json.Marshal(map[string]any{
		"deltas": []map[string]any{
			{"key_uuid": key, "bytes_up": 100, "bytes_down": 200},
		},
	})
	resp, err := client.Post(ts.URL+"/v1/sync/deltas", "application/json", bytes.NewReader(body))
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
	if m["accepted"] != true {
		t.Fatalf("accepted = %v", m["accepted"])
	}
}

func TestHeartbeatEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := svc.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"agent_version": "0.1.0", "xray_version": "25.1.1"})
	resp, err := client.Post(ts.URL+"/v1/sync/heartbeat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got, err := svc.GetNodeByCN(ctx, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentVersion == nil || *got.AgentVersion != "0.1.0" {
		t.Fatalf("agent version not saved: %+v", got)
	}
}
