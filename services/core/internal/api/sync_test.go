package api

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestSyncUsersEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleNode)
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
	ts, client, _ := newAPITestEnv(t, mtls.RoleNode)
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
	ts, client, svc := newAPITestEnv(t, mtls.RoleNode)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := svc.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	u, key, _ := svc.RegisterUser(ctx, 601)
	_ = u
	body, _ := json.Marshal(map[string]any{
		"batch_id": "b-1",
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
	if m["accepted"] != true || m["duplicate"] != false {
		t.Fatalf("resp = %v, want accepted and not duplicate", m)
	}
}

func TestHeartbeatEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleNode)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := svc.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"agent_version": "0.1.0", "xray_version": "25.1.1", "agent_uptime_sec": 3600, "xray_uptime_sec": 86400, "active_users": 412}
	if status := postJSON(t, client, ts.URL+"/v1/sync/heartbeat", body, nil); status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	got, err := svc.GetNodeByCN(ctx, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentVersion == nil || *got.AgentVersion != "0.1.0" ||
		got.XrayVersion == nil || *got.XrayVersion != "25.1.1" ||
		got.AgentUptimeSec == nil || *got.AgentUptimeSec != 3600 ||
		got.XrayUptimeSec == nil || *got.XrayUptimeSec != 86400 ||
		got.ActiveUsers == nil || *got.ActiveUsers != 412 {
		t.Fatalf("heartbeat not saved: %+v", got)
	}
}

func TestHeartbeatUnknownXrayState(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleNode)
	ctx := context.Background()
	if err := svc.UpsertNode(ctx, &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"agent_version": "0.1.0", "xray_version": "", "agent_uptime_sec": 5, "xray_uptime_sec": nil, "active_users": nil}
	if status := postJSON(t, client, ts.URL+"/v1/sync/heartbeat", body, nil); status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	got, err := svc.GetNodeByCN(ctx, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveUsers != nil || got.XrayUptimeSec != nil || got.XrayVersion != nil {
		t.Fatalf("unknown xray state must be stored as NULL: %+v", got)
	}
}

func TestHeartbeatValidation(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleNode)
	if err := svc.UpsertNode(context.Background(), &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	cases := map[string]map[string]any{
		"negative agent uptime": {"agent_version": "v", "agent_uptime_sec": -1},
		"negative xray uptime":  {"agent_version": "v", "xray_uptime_sec": -1},
		"negative users":        {"agent_version": "v", "active_users": -1},
		"users overflow int":    {"agent_version": "v", "active_users": int64(math.MaxInt32) + 1},
	}
	for name, body := range cases {
		if status := postJSON(t, client, ts.URL+"/v1/sync/heartbeat", body, nil); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, status)
		}
	}
}

func TestSyncDeltasDuplicateBatch(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleNode)
	ctx := context.Background()
	if err := svc.UpsertNode(ctx, &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	u, key, _ := svc.RegisterUser(ctx, 602)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenewSubscription(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"batch_id": "b-dup", "deltas": []map[string]any{{"key_uuid": key, "bytes_up": 100, "bytes_down": 200}}}
	var first, second map[string]any
	if status := postJSON(t, client, ts.URL+"/v1/sync/deltas", body, &first); status != http.StatusOK {
		t.Fatalf("first status = %d", status)
	}
	if status := postJSON(t, client, ts.URL+"/v1/sync/deltas", body, &second); status != http.StatusOK {
		t.Fatalf("second status = %d", status)
	}
	if first["duplicate"] != false || second["duplicate"] != true {
		t.Fatalf("first = %v, second = %v", first, second)
	}
	_, _, sub, _, err := svc.GetUserFullByToken(ctx, u.SubToken)
	if err != nil {
		t.Fatal(err)
	}
	if sub.TrafficUsedBytes != 300 {
		t.Fatalf("traffic_used = %d, want 300", sub.TrafficUsedBytes)
	}
}

func TestSyncDeltasValidation(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleNode)
	if err := svc.UpsertNode(context.Background(), &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	okDelta := []map[string]any{{"key_uuid": "k", "bytes_up": 1, "bytes_down": 1}}
	cases := map[string]map[string]any{
		"no batch_id":    {"deltas": okDelta},
		"long batch_id":  {"batch_id": strings.Repeat("x", 65), "deltas": okDelta},
		"negative up":    {"batch_id": "b-neg-up", "deltas": []map[string]any{{"key_uuid": "k", "bytes_up": -1, "bytes_down": 0}}},
		"negative down":  {"batch_id": "b-neg-down", "deltas": []map[string]any{{"key_uuid": "k", "bytes_up": 0, "bytes_down": -5}}},
		"not an integer": {"batch_id": "b-float", "deltas": []map[string]any{{"key_uuid": "k", "bytes_up": 1.5, "bytes_down": 0}}},
	}
	for name, body := range cases {
		if status := postJSON(t, client, ts.URL+"/v1/sync/deltas", body, nil); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, status)
		}
	}
}

func TestSyncDeltasEmptyBatch(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleNode)
	if err := svc.UpsertNode(context.Background(), &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"batch_id": "b-empty", "deltas": []map[string]any{}}
	var first, second map[string]any
	if status := postJSON(t, client, ts.URL+"/v1/sync/deltas", body, &first); status != http.StatusOK {
		t.Fatalf("empty batch status = %d, want 200", status)
	}
	if status := postJSON(t, client, ts.URL+"/v1/sync/deltas", body, &second); status != http.StatusOK {
		t.Fatalf("repeat status = %d, want 200", status)
	}
	if first["duplicate"] != false || second["duplicate"] != true {
		t.Fatalf("first = %v, second = %v", first, second)
	}
}

func TestSyncDeltasUnknownNodeWritesNothing(t *testing.T) {
	e := newAPIEnv(t)
	ghost := e.client(t, "node-ghost", mtls.RoleNode)
	body := map[string]any{"batch_id": "b-ghost", "deltas": []map[string]any{{"key_uuid": "k", "bytes_up": 1, "bytes_down": 1}}}
	var resp map[string]any
	status := postJSON(t, ghost, e.ts.URL+"/v1/sync/deltas", body, &resp)
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", status)
	}
	if err := e.svc.UpsertNode(context.Background(), &model.Node{CertCN: "node-ghost", Name: "late"}); err != nil {
		t.Fatal(err)
	}
	if status := postJSON(t, ghost, e.ts.URL+"/v1/sync/deltas", body, &resp); status != http.StatusOK || resp["duplicate"] != false {
		t.Fatalf("after registration: status %d resp %v; rejected batch must not have been recorded", status, resp)
	}
}
