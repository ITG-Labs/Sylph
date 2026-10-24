package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

type addressView struct {
	ID         int64   `json:"id"`
	NodeID     int64   `json:"node_id"`
	NodeName   string  `json:"node_name"`
	IP         string  `json:"ip"`
	Status     string  `json:"status"`
	Error      *string `json:"error"`
	Assignment *struct {
		UserID int64  `json:"user_id"`
		Note   string `json:"note"`
		Source string `json:"source"`
	} `json:"assignment"`
	BalancerWarning bool `json:"balancer_warning"`
}

func (e *adminEnv) addresses(t *testing.T) []addressView {
	t.Helper()
	r := e.call(t, http.MethodGet, "/v1/admin/addresses", nil)
	mustStatus(t, r, http.StatusOK)
	var out struct {
		Addresses []addressView `json:"addresses"`
	}
	r.decode(t, &out)
	return out.Addresses
}

func heartbeatWith(addresses any) map[string]any {
	return map[string]any{"agent_version": "0.2.0", "agent_uptime_sec": 1, "addresses": addresses}
}

func TestHeartbeatReportsAddresses(t *testing.T) {
	e := newAdminEnv(t)
	e.createNode(t, "node-1", nil)
	node := e.client(t, "node-1", mtls.RoleNode)
	url := e.ts.URL + "/v1/sync/heartbeat"

	ok := postJSON(t, node, url, heartbeatWith([]map[string]any{
		{"ip": "203.0.113.7", "ok": true},
		{"ip": "203.0.113.8", "ok": false, "error": "egress_network_missing"},
	}), nil)
	if ok != http.StatusOK {
		t.Fatalf("heartbeat status = %d", ok)
	}
	list := e.addresses(t)
	if len(list) != 2 || list[0].IP != "203.0.113.7" || list[0].Status != "ok" || list[0].NodeName != "node-1" ||
		list[1].Status != "error" || list[1].Error == nil || *list[1].Error != "egress_network_missing" {
		t.Fatalf("addresses = %+v", list)
	}

	if status := postJSON(t, node, url, map[string]any{"agent_version": "0.1.0", "agent_uptime_sec": 1}, nil); status != http.StatusOK {
		t.Fatalf("old agent heartbeat = %d", status)
	}
	if list := e.addresses(t); len(list) != 2 || list[0].Status != "ok" {
		t.Fatalf("heartbeat without addresses changed them: %+v", list)
	}

	if status := postJSON(t, node, url, heartbeatWith([]map[string]any{}), nil); status != http.StatusOK {
		t.Fatalf("empty addresses heartbeat = %d", status)
	}
	for _, a := range e.addresses(t) {
		if a.Status != "missing" {
			t.Fatalf("address after empty report = %+v", a)
		}
	}

	many := make([]map[string]any, 65)
	for i := range many {
		many[i] = map[string]any{"ip": fmt.Sprintf("10.0.%d.%d", i/250, i%250+1), "ok": true}
	}
	for name, addrs := range map[string]any{
		"ipv6":          []map[string]any{{"ip": "2001:db8::1", "ok": true}},
		"not canonical": []map[string]any{{"ip": "203.0.113.07", "ok": true}},
		"not an ip":     []map[string]any{{"ip": "node.example.com", "ok": true}},
		"duplicate":     []map[string]any{{"ip": "203.0.113.7", "ok": true}, {"ip": "203.0.113.7", "ok": false}},
		"too many":      many,
	} {
		if status := postJSON(t, node, url, heartbeatWith(addrs), nil); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, status)
		}
	}
}

func TestDedicatedAddressFlow(t *testing.T) {
	e := newAdminEnv(t)
	ctx := t.Context()
	nodeID := e.createNode(t, "node-1", nil)
	node := e.client(t, "node-1", mtls.RoleNode)
	bot := e.client(t, "bot", mtls.RoleBot)
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/heartbeat", heartbeatWith([]map[string]any{{"ip": "203.0.113.7", "ok": true}, {"ip": "203.0.113.8", "ok": true}}), nil); status != http.StatusOK {
		t.Fatalf("heartbeat = %d", status)
	}
	addr, spare := e.addresses(t)[0], e.addresses(t)[1]

	r := e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "Выделенный IP", "price_rub": 900, "period_days": 30, "visibility": "private", "dedicated_ip": true})
	mustStatus(t, r, http.StatusOK)
	var plan struct {
		ID          int64 `json:"id"`
		DedicatedIP bool  `json:"dedicated_ip"`
	}
	r.decode(t, &plan)
	if !plan.DedicatedIP {
		t.Fatalf("created plan = %s", r.body)
	}

	var client, other struct {
		ID int64 `json:"id"`
	}
	if status := postJSON(t, bot, e.ts.URL+"/v1/users", map[string]any{"telegram_id": 5001}, &client); status != http.StatusOK {
		t.Fatalf("create user = %d", status)
	}
	if status := postJSON(t, bot, e.ts.URL+"/v1/users", map[string]any{"telegram_id": 5002}, &other); status != http.StatusOK {
		t.Fatalf("create user = %d", status)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) VALUES ($1, $2, now(), now() + interval '30 days')`,
		client.ID, plan.ID); err != nil {
		t.Fatal(err)
	}
	var key string
	if err := e.pool.QueryRow(ctx, `SELECT uuid FROM user_keys WHERE user_id = $1`, client.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}

	path := fmt.Sprintf("/v1/admin/addresses/%d", addr.ID)
	mustField(t, e.call(t, http.MethodPut, path+"/assignment", map[string]any{"user_id": 999999}), http.StatusBadRequest, "bad_request", "user_id")
	mustField(t, e.call(t, http.MethodPut, path+"/assignment", map[string]any{"user_id": client.ID, "note": strings.Repeat("я", 201)}), http.StatusBadRequest, "bad_request", "note")
	mustStatus(t, e.call(t, http.MethodPut, "/v1/admin/addresses/999999/assignment", map[string]any{"user_id": client.ID}), http.StatusNotFound)

	r = e.call(t, http.MethodPut, path+"/assignment", map[string]any{"user_id": client.ID, "note": "клиент"})
	mustStatus(t, r, http.StatusOK)
	var assigned addressView
	r.decode(t, &assigned)
	if assigned.Assignment == nil || assigned.Assignment.UserID != client.ID || assigned.Assignment.Note != "клиент" || assigned.Assignment.Source != "admin" {
		t.Fatalf("assigned = %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPut, path+"/assignment", map[string]any{"user_id": other.ID}), http.StatusConflict, "conflict", "address")

	var sync struct {
		Users []struct {
			UUID     string `json:"uuid"`
			EgressIP string `json:"egress_ip"`
		} `json:"users"`
	}
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/users", map[string]any{"known_version": 0}, &sync); status != http.StatusOK {
		t.Fatalf("sync = %d", status)
	}
	if len(sync.Users) != 1 || sync.Users[0].UUID != key || sync.Users[0].EgressIP != "203.0.113.7" {
		t.Fatalf("sync users = %+v", sync.Users)
	}

	r = e.call(t, http.MethodGet, fmt.Sprintf("/v1/admin/users/%d", client.ID), nil)
	mustStatus(t, r, http.StatusOK)
	var card struct {
		DedicatedPlan bool `json:"dedicated_plan"`
		Addresses     []struct {
			AddressID int64  `json:"address_id"`
			NodeID    int64  `json:"node_id"`
			IP        string `json:"ip"`
			Effective bool   `json:"effective"`
			Reason    string `json:"reason"`
		} `json:"addresses"`
	}
	r.decode(t, &card)
	if !card.DedicatedPlan || len(card.Addresses) != 1 || !card.Addresses[0].Effective || card.Addresses[0].NodeID != nodeID || card.Addresses[0].Reason != "" {
		t.Fatalf("card = %s", r.body)
	}

	var status struct {
		DedicatedIPs []struct {
			IP       string `json:"ip"`
			NodeName string `json:"node_name"`
		} `json:"dedicated_ips"`
	}
	if code := getJSON(t, bot, e.ts.URL+"/v1/users/5001", &status); code != http.StatusOK {
		t.Fatalf("bot status = %d", code)
	}
	if len(status.DedicatedIPs) != 1 || status.DedicatedIPs[0].IP != "203.0.113.7" || status.DedicatedIPs[0].NodeName != "node-1" {
		t.Fatalf("bot dedicated ips = %+v", status.DedicatedIPs)
	}
	mustStatus(t, e.call(t, http.MethodPut, fmt.Sprintf("/v1/admin/addresses/%d/assignment", spare.ID), map[string]any{"user_id": other.ID}), http.StatusOK)
	var plain struct {
		DedicatedIPs []any `json:"dedicated_ips"`
	}
	if code := getJSON(t, bot, e.ts.URL+"/v1/users/5002", &plain); code != http.StatusOK || plain.DedicatedIPs == nil || len(plain.DedicatedIPs) != 0 {
		t.Fatalf("bot status without address = %d %+v", code, plain)
	}

	mustField(t, e.call(t, http.MethodDelete, path, nil), http.StatusConflict, "conflict", "address")
	mustStatus(t, e.call(t, http.MethodDelete, path+"/assignment", nil), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodDelete, path+"/assignment", nil), http.StatusNotFound)
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/heartbeat", heartbeatWith([]map[string]any{}), nil); status != http.StatusOK {
		t.Fatalf("heartbeat = %d", status)
	}
	mustStatus(t, e.call(t, http.MethodDelete, path, nil), http.StatusOK)
	if list := e.addresses(t); len(list) != 1 || list[0].IP != "203.0.113.8" {
		t.Fatalf("addresses after delete = %+v", list)
	}

	var actions []string
	rows, err := e.pool.Query(ctx, `SELECT action FROM admin_audit WHERE entity = 'address' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, a)
	}
	if fmt.Sprint(actions) != "[assign assign unassign delete]" {
		t.Fatalf("audit actions = %v", actions)
	}
}

func TestAddressBalancerWarning(t *testing.T) {
	e := newAdminEnv(t)
	nodeID := e.createNode(t, "node-1", nil)
	node := e.client(t, "node-1", mtls.RoleNode)
	if status := postJSON(t, node, e.ts.URL+"/v1/sync/heartbeat", heartbeatWith([]map[string]any{{"ip": "203.0.113.7", "ok": true}}), nil); status != http.StatusOK {
		t.Fatalf("heartbeat = %d", status)
	}
	plain := profileDoc()
	r := e.call(t, http.MethodPost, "/v1/admin/profiles", map[string]any{"name": "plain", "doc": plain})
	mustStatus(t, r, http.StatusOK)
	var p profileResp
	r.decode(t, &p)
	mustStatus(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/nodes/%d", nodeID), map[string]any{"profile_id": p.ID}), http.StatusOK)
	if a := e.addresses(t); a[0].BalancerWarning {
		t.Fatalf("warning on a profile without balancers: %+v", a)
	}
	balanced := profileDoc(map[string]any{"tag": "de", "kind": "proxy", "proxy": map[string]any{"protocol": "socks", "address": "de.example.com", "port": 1080}})
	balanced["balancers"] = []any{map[string]any{"tag": "bal", "strategy": "random", "outbounds": []string{"direct", "de"}}}
	balanced["default"] = "bal"
	mustStatus(t, e.call(t, http.MethodPut, fmt.Sprintf("/v1/admin/profiles/%d", p.ID), map[string]any{"name": "plain", "doc": balanced, "revision": 1}), http.StatusOK)
	if a := e.addresses(t); !a[0].BalancerWarning {
		t.Fatalf("no warning with a direct balancer as default: %+v", a)
	}
}
