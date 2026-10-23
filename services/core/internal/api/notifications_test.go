package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

type dueNotification struct {
	UserID            int64      `json:"user_id"`
	TelegramID        int64      `json:"telegram_id"`
	Kind              string     `json:"kind"`
	Ref               string     `json:"ref"`
	Trial             bool       `json:"trial"`
	PaidUntil         *time.Time `json:"paid_until"`
	PeriodEnds        *time.Time `json:"period_ends"`
	TrafficUsedBytes  *int64     `json:"traffic_used_bytes"`
	TrafficLimitBytes *int64     `json:"traffic_limit_bytes"`
}

func TestNotificationsDueAndSent(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	bot := e.client(t, "bot", mtls.RoleBot)
	u, p := botUserAndPlan(t, e.apiEnv, 8301, 199)
	if _, err := e.pool.Exec(ctx, `UPDATE plans SET traffic_limit_bytes = 1000 WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at, traffic_used_bytes) VALUES ($1, $2, now() - interval '1 day', now() + interval '20 days', 960)`,
		u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	r := adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/notifications/due", nil)
	mustStatus(t, r, http.StatusOK)
	var due []dueNotification
	r.decode(t, &due)
	if len(due) != 1 || due[0].Kind != "traffic_95" || due[0].UserID != u.ID || due[0].TelegramID != 8301 || due[0].Trial ||
		due[0].TrafficUsedBytes == nil || *due[0].TrafficUsedBytes != 960 || *due[0].TrafficLimitBytes != 1000 ||
		due[0].PeriodEnds == nil || due[0].PaidUntil != nil || !strings.HasSuffix(due[0].Ref, ":1000") {
		t.Fatalf("due = %s", r.body)
	}
	sent := map[string]any{"user_id": u.ID, "kind": due[0].Kind, "ref": due[0].Ref, "outcome": "sent"}
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/notifications/sent", sent), http.StatusNoContent)
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/notifications/sent", sent), http.StatusNoContent)
	r = adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/notifications/due", nil)
	mustStatus(t, r, http.StatusOK)
	if strings.TrimSpace(string(r.body)) != "[]" {
		t.Fatalf("after sent = %s", r.body)
	}
	for _, body := range []map[string]any{
		{"user_id": u.ID, "kind": "traffic_50", "ref": due[0].Ref, "outcome": "sent"},
		{"user_id": u.ID, "kind": "traffic_80", "ref": "x", "outcome": "sent"},
		{"user_id": u.ID, "kind": "traffic_80", "ref": due[0].Ref, "outcome": "lost"},
	} {
		mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/notifications/sent", body), http.StatusBadRequest)
	}
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/notifications/sent",
		map[string]any{"user_id": u.ID, "kind": "traffic_80", "ref": due[0].Ref, "outcome": "sent", "extra": 1}), http.StatusBadRequest)
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/notifications/sent",
		map[string]any{"user_id": u.ID + 1000, "kind": "traffic_80", "ref": due[0].Ref, "outcome": "sent"}), http.StatusNotFound)
}

func TestNotificationsOnlyForBot(t *testing.T) {
	e := newAPIEnv(t)
	for _, role := range []string{mtls.RolePanel, mtls.RoleSubs, mtls.RoleCron, mtls.RoleMonitor} {
		c := e.client(t, role, role)
		mustStatus(t, adminCall(t, c, http.MethodGet, e.ts.URL+"/v1/notifications/due", nil), http.StatusForbidden)
		mustStatus(t, adminCall(t, c, http.MethodPost, e.ts.URL+"/v1/notifications/sent",
			map[string]any{"user_id": 1, "kind": "expired", "ref": "1", "outcome": "sent"}), http.StatusForbidden)
	}
	node := e.client(t, "node-1", mtls.RoleNode)
	mustStatus(t, adminCall(t, node, http.MethodGet, e.ts.URL+"/v1/notifications/due", nil), http.StatusForbidden)
}

func TestAdminSettingsNotifyToggles(t *testing.T) {
	e := newAdminEnv(t)
	var st struct {
		Traffic bool   `json:"notify_traffic"`
		Expiry  bool   `json:"notify_expiry"`
		Trial   bool   `json:"notify_trial"`
		Title   string `json:"subs_title"`
	}
	r := e.call(t, http.MethodGet, "/v1/admin/settings", nil)
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &st)
	if !st.Traffic || !st.Expiry || !st.Trial {
		t.Fatalf("defaults = %s", r.body)
	}
	mustStatus(t, e.call(t, http.MethodPatch, "/v1/admin/settings", map[string]any{"notify_trial": "no"}), http.StatusBadRequest)
	r = e.call(t, http.MethodPatch, "/v1/admin/settings", map[string]any{"notify_trial": false})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &st)
	if !st.Traffic || !st.Expiry || st.Trial || st.Title != "Sylph" {
		t.Fatalf("after trial off = %s", r.body)
	}
	entries := auditList(t, e, "?entity=settings")
	if len(entries) != 1 || entries[0].Action != "update" || entries[0].Details["notify_trial"] != false {
		t.Fatalf("audit = %+v", entries)
	}
}
