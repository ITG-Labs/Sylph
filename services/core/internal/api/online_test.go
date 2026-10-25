package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestOnlineFromHeartbeatToPanel(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	node := e.client(t, "node-on", mtls.RoleNode)
	n := &model.Node{CertCN: "node-on", Name: "Германия"}
	if err := e.svc.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	u, _, err := e.svc.RegisterUser(ctx, 8601)
	if err != nil {
		t.Fatal(err)
	}
	var key string
	if err := e.pool.QueryRow(ctx, `SELECT uuid FROM user_keys WHERE user_id = $1`, u.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	hb := map[string]any{"agent_version": "0.3.0", "agent_uptime_sec": 5, "online": []map[string]any{
		{"key": key, "ips": []map[string]any{{"ip": "203.0.113.5", "age_sec": 600}, {"ip": "2001:db8::5", "age_sec": 0}}},
		{"key": "ffffffff-ffff-4fff-8fff-ffffffffffff", "ips": []map[string]any{{"ip": "192.0.2.1", "age_sec": 1}}},
	}}
	mustStatus(t, adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/heartbeat", hb), http.StatusOK)

	var card struct {
		Presence struct {
			Online bool
			Now    []struct {
				NodeName string `json:"node_name"`
				IPs      []struct{ IP string }
			}
		}
	}
	r := adminCall(t, e.panel, http.MethodGet, fmt.Sprintf("%s/v1/admin/users/%d", e.ts.URL, u.ID), nil)
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &card)
	if !card.Presence.Online || len(card.Presence.Now) != 1 || card.Presence.Now[0].NodeName != "Германия" || len(card.Presence.Now[0].IPs) != 2 {
		t.Fatalf("card presence = %s", r.body)
	}
	r = adminCall(t, e.panel, http.MethodGet, e.ts.URL+"/v1/admin/users?online=1", nil)
	mustStatus(t, r, http.StatusOK)
	if !strings.Contains(string(r.body), `"online_nodes":[{"id":`) {
		t.Fatalf("online list = %s", r.body)
	}
	mustStatus(t, adminCall(t, e.panel, http.MethodGet, e.ts.URL+"/v1/admin/users?online=yes", nil), http.StatusBadRequest)
	r = adminCall(t, e.panel, http.MethodGet, fmt.Sprintf("%s/v1/admin/users/%d/ips?limit=1", e.ts.URL, u.ID), nil)
	mustStatus(t, r, http.StatusOK)
	if !strings.Contains(string(r.body), `"total":2`) {
		t.Fatalf("ip history = %s", r.body)
	}
	mustStatus(t, adminCall(t, e.panel, http.MethodGet, e.ts.URL+"/v1/admin/users/999999/ips", nil), http.StatusNotFound)
	r = adminCall(t, e.panel, http.MethodGet, fmt.Sprintf("%s/v1/admin/nodes/%d/online", e.ts.URL, n.ID), nil)
	mustStatus(t, r, http.StatusOK)
	if !strings.Contains(string(r.body), `"telegram_id":8601`) {
		t.Fatalf("node online = %s", r.body)
	}
	r = adminCall(t, e.panel, http.MethodGet, e.ts.URL+"/v1/admin/overview", nil)
	mustStatus(t, r, http.StatusOK)
	if !strings.Contains(string(r.body), `"online":1`) || !strings.Contains(string(r.body), `"online_users":1`) {
		t.Fatalf("overview = %s", r.body)
	}

	for name, bad := range map[string][]map[string]any{
		"zone":      {{"key": key, "ips": []map[string]any{{"ip": "fe80::1%eth0", "age_sec": 1}}}},
		"short v6":  {{"key": key, "ips": []map[string]any{{"ip": "2001:DB8::5", "age_sec": 1}}}},
		"not an ip": {{"key": key, "ips": []map[string]any{{"ip": "localhost", "age_sec": 1}}}},
		"negative":  {{"key": key, "ips": []map[string]any{{"ip": "203.0.113.5", "age_sec": -1}}}},
		"empty key": {{"key": "", "ips": []map[string]any{{"ip": "203.0.113.5", "age_sec": 1}}}},
	} {
		body := map[string]any{"agent_version": "0.3.0", "online": bad}
		if r := adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/heartbeat", body); r.status != http.StatusBadRequest {
			t.Fatalf("%s: status %d", name, r.status)
		}
	}
	mustStatus(t, adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/heartbeat", map[string]any{"agent_version": "0.2.0"}), http.StatusOK)
	r = adminCall(t, e.panel, http.MethodGet, fmt.Sprintf("%s/v1/admin/users/%d", e.ts.URL, u.ID), nil)
	r.decode(t, &card)
	if !card.Presence.Online {
		t.Fatal("a heartbeat without online data must not clear the snapshot")
	}
	mustStatus(t, adminCall(t, node, http.MethodPost, e.ts.URL+"/v1/sync/heartbeat", map[string]any{"agent_version": "0.3.0", "online": []any{}}), http.StatusOK)
	r = adminCall(t, e.panel, http.MethodGet, fmt.Sprintf("%s/v1/admin/users/%d", e.ts.URL, u.ID), nil)
	r.decode(t, &card)
	if card.Presence.Online {
		t.Fatal("an empty online report must clear the node")
	}
}
