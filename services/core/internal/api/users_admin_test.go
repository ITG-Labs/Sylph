package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func mustCode(t *testing.T, r adminResp, status int, code string) {
	t.Helper()
	mustStatus(t, r, status)
	if got, _ := r.errorField(t); got != code {
		t.Fatalf("code = %q, want %q: %s", got, code, r.body)
	}
}

func TestBanMakesTheUserDisappear(t *testing.T) {
	e := newAdminEnv(t)
	u, p := botUserAndPlan(t, e.apiEnv, 7001, 199)
	if _, err := e.svc.RecordPayment(context.Background(), "gift-1", u.ID, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	bot := e.client(t, "bot", mtls.RoleBot)
	subs := e.client(t, "subs", mtls.RoleSubs)
	base := e.ts.URL
	path := fmt.Sprintf("/v1/admin/users/%d", u.ID)

	r := e.call(t, http.MethodPost, path+"/ban", map[string]any{"reason": "spam"})
	mustStatus(t, r, http.StatusOK)
	var ban struct {
		OK      bool `json:"ok"`
		Already bool `json:"already"`
	}
	r.decode(t, &ban)
	if !ban.OK || ban.Already {
		t.Fatalf("ban = %s", r.body)
	}
	userPath := fmt.Sprintf("/v1/users/%d", u.ID)
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/v1/users", map[string]any{"telegram_id": 7001}},
		{http.MethodGet, "/v1/users/7001", nil},
		{http.MethodGet, userPath + "/plans", nil},
		{http.MethodGet, userPath + "/stats", nil},
		{http.MethodGet, userPath + "/devices", nil},
		{http.MethodPost, userPath + "/devices/reset", nil},
		{http.MethodDelete, userPath + "/devices/1", nil},
		{http.MethodPost, userPath + "/rotate", nil},
		{http.MethodPost, userPath + "/trial", nil},
		{http.MethodPost, "/v1/invites/redeem", map[string]any{"user_id": u.ID, "code": "AAAAAAAAAAAAAAAAAAAAAA"}},
	} {
		r := adminCall(t, bot, c.method, base+c.path, c.body)
		if r.status != http.StatusForbidden {
			t.Fatalf("%s %s: status %d, want 403: %s", c.method, c.path, r.status, r.body)
		}
		mustCode(t, r, http.StatusForbidden, "banned")
	}
	r = adminCall(t, subs, http.MethodPost, base+"/v1/subscription", map[string]any{"sub_token": u.SubToken})
	mustStatus(t, r, http.StatusNotFound)

	r = e.call(t, http.MethodPost, path+"/ban", map[string]any{})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &ban)
	if !ban.Already {
		t.Fatalf("second ban = %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPost, "/v1/payments", map[string]any{"payment_id": "gift-2", "user_id": u.ID, "plan_id": p.ID, "amount_rub": 0}),
		http.StatusConflict, "conflict", "user_id")

	r = e.call(t, http.MethodPost, path+"/unban", nil)
	mustStatus(t, r, http.StatusOK)
	mustStatus(t, adminCall(t, bot, http.MethodGet, base+"/v1/users/7001", nil), http.StatusOK)
	r = adminCall(t, subs, http.MethodPost, base+"/v1/subscription", map[string]any{"sub_token": u.SubToken})
	mustStatus(t, r, http.StatusOK)
	var view struct {
		Status string `json:"status"`
	}
	r.decode(t, &view)
	if view.Status != "expired" {
		t.Fatalf("after unban the subscription is %q, want expired", view.Status)
	}

	mustField(t, e.call(t, http.MethodPost, path+"/ban", map[string]any{"reason": "x", "typo": 1}), http.StatusBadRequest, "bad_request", "")
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/users/999999/ban", map[string]any{}), http.StatusNotFound)
	mustField(t, e.call(t, http.MethodPost, "/v1/admin/users/abc/ban", map[string]any{}), http.StatusBadRequest, "bad_request", "id")
}

func TestManageSubscriptionThroughTheAPI(t *testing.T) {
	e := newAdminEnv(t)
	u, p := botUserAndPlan(t, e.apiEnv, 7002, 199)
	path := fmt.Sprintf("/v1/admin/users/%d", u.ID)

	mustField(t, e.call(t, http.MethodPost, path+"/extend", map[string]any{"days": 5}), http.StatusConflict, "conflict", "subscription")
	if _, err := e.svc.RecordPayment(context.Background(), "gift-1", u.ID, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RecordPayment(context.Background(), "gift-2", u.ID, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	mustField(t, e.call(t, http.MethodPost, path+"/extend", map[string]any{"days": 0}), http.StatusBadRequest, "bad_request", "days")
	r := e.call(t, http.MethodPost, path+"/extend", map[string]any{"days": 5})
	mustStatus(t, r, http.StatusOK)
	var sub struct {
		ID                int64  `json:"id"`
		TrafficLimitBytes *int64 `json:"traffic_limit_bytes"`
		PaidUntil         string `json:"paid_until"`
	}
	r.decode(t, &sub)
	if sub.ID == 0 || sub.PaidUntil == "" {
		t.Fatalf("extend answered %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPost, path+"/traffic", map[string]any{"bytes": 1000}), http.StatusConflict, "conflict", "bytes")
	mustField(t, e.call(t, http.MethodPost, path+"/traffic", map[string]any{"bytes": -1}), http.StatusBadRequest, "bad_request", "bytes")

	limit := int64(1000)
	other := map[string]any{"name": "limited", "price_rub": 1, "period_days": 30, "traffic_limit_bytes": limit}
	r = e.call(t, http.MethodPost, "/v1/admin/plans", other)
	mustStatus(t, r, http.StatusOK)
	var plan struct {
		ID int64 `json:"id"`
	}
	r.decode(t, &plan)
	r = e.call(t, http.MethodPost, path+"/plan", map[string]any{"plan_id": plan.ID})
	mustStatus(t, r, http.StatusOK)
	var changed struct {
		Changed bool `json:"changed"`
	}
	r.decode(t, &changed)
	if !changed.Changed {
		t.Fatalf("plan change = %s", r.body)
	}
	r = e.call(t, http.MethodPost, path+"/plan", map[string]any{"plan_id": plan.ID})
	r.decode(t, &changed)
	if changed.Changed {
		t.Fatal("same plan reported as changed")
	}
	mustStatus(t, e.call(t, http.MethodPost, path+"/plan", map[string]any{"plan_id": 999999}), http.StatusNotFound)
	r = e.call(t, http.MethodPost, path+"/traffic", map[string]any{"bytes": 500})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &sub)
	if sub.TrafficLimitBytes == nil || *sub.TrafficLimitBytes != 1500 {
		t.Fatalf("traffic bonus answered %s", r.body)
	}

	closePath := fmt.Sprintf("/v1/admin/periods/%d/close", sub.ID)
	mustStatus(t, e.call(t, http.MethodPost, closePath, nil), http.StatusOK)
	mustField(t, e.call(t, http.MethodPost, closePath, nil), http.StatusConflict, "conflict", "id")
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/periods/999999/close", nil), http.StatusNotFound)
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/users/999999/extend", map[string]any{"days": 1}), http.StatusNotFound)
}

func TestUsersListAndCard(t *testing.T) {
	e := newAdminEnv(t)
	a, p := botUserAndPlan(t, e.apiEnv, 8001, 199)
	b, _ := botUserAndPlan(t, e.apiEnv, 8002, 199)
	if _, err := e.svc.RecordPayment(context.Background(), "gift-1", a.ID, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	type page struct {
		Users []struct {
			ID         int64  `json:"id"`
			TelegramID int64  `json:"telegram_id"`
			SubToken   string `json:"sub_token"`
			Plan       *struct {
				Name string `json:"name"`
			} `json:"plan"`
		} `json:"users"`
		NextBefore *int64 `json:"next_before"`
	}
	list := func(query string) page {
		t.Helper()
		r := e.call(t, http.MethodGet, "/v1/admin/users"+query, nil)
		mustStatus(t, r, http.StatusOK)
		var pg page
		r.decode(t, &pg)
		return pg
	}
	all := list("")
	if len(all.Users) != 2 || all.Users[0].ID != b.ID || all.NextBefore != nil {
		t.Fatalf("all = %+v", all)
	}
	if all.Users[1].Plan == nil || all.Users[1].Plan.Name != p.Name || all.Users[0].SubToken != "" {
		t.Fatalf("rows = %+v", all.Users)
	}
	if got := list("?q=8001"); len(got.Users) != 1 || got.Users[0].ID != a.ID {
		t.Fatalf("by telegram id = %+v", got)
	}
	if got := list(fmt.Sprintf("?q=%d", b.ID)); len(got.Users) != 1 || got.Users[0].ID != b.ID {
		t.Fatalf("by id = %+v", got)
	}
	if got := list("?q=https://subs.example.com/" + a.SubToken + "%3Fx%3D1"); len(got.Users) != 1 || got.Users[0].ID != a.ID {
		t.Fatalf("by link = %+v", got)
	}
	if got := list("?q=" + b.SubToken); len(got.Users) != 1 || got.Users[0].ID != b.ID {
		t.Fatalf("by token = %+v", got)
	}
	if got := list("?subscription=none"); len(got.Users) != 1 || got.Users[0].ID != b.ID {
		t.Fatalf("without a subscription = %+v", got)
	}
	first := list("?limit=1")
	if len(first.Users) != 1 || first.NextBefore == nil || *first.NextBefore != b.ID {
		t.Fatalf("first page = %+v", first)
	}
	if got := list(fmt.Sprintf("?limit=1&before=%d", *first.NextBefore)); len(got.Users) != 1 || got.Users[0].ID != a.ID {
		t.Fatalf("second page = %+v", got)
	}
	for _, bad := range []struct{ query, field string }{
		{"?limit=0", "limit"}, {"?limit=101", "limit"}, {"?status=gone", "status"}, {"?subscription=maybe", "subscription"},
		{"?plan_id=x", "plan_id"}, {"?before=-1", "before"}, {"?q=99999999999999999999", "q"}, {"?sort=id", "sort"},
	} {
		mustField(t, e.call(t, http.MethodGet, "/v1/admin/users"+bad.query, nil), http.StatusBadRequest, "bad_request", bad.field)
	}

	r := e.call(t, http.MethodGet, fmt.Sprintf("/v1/admin/users/%d", a.ID), nil)
	mustStatus(t, r, http.StatusOK)
	var card struct {
		User struct {
			SubToken string `json:"sub_token"`
			Status   string `json:"status"`
		} `json:"user"`
		Periods []struct {
			State     string  `json:"state"`
			PlanName  string  `json:"plan_name"`
			PaymentID *string `json:"payment_id"`
		} `json:"periods"`
		Payments []struct {
			PaymentID string `json:"payment_id"`
		} `json:"payments"`
		Devices []any `json:"devices"`
		Access  []any `json:"access"`
	}
	r.decode(t, &card)
	if card.User.SubToken != a.SubToken || len(card.Periods) != 1 || card.Periods[0].State != "current" || card.Periods[0].PaymentID == nil || *card.Periods[0].PaymentID != "gift-1" {
		t.Fatalf("card = %s", r.body)
	}
	if len(card.Payments) != 1 || card.Devices == nil || card.Access == nil {
		t.Fatalf("card lists = %s", r.body)
	}
	mustStatus(t, e.call(t, http.MethodGet, "/v1/admin/users/999999", nil), http.StatusNotFound)
}

func TestByTokenAnswersUsersWithoutAPeriod(t *testing.T) {
	e := newAdminEnv(t)
	u, _ := botUserAndPlan(t, e.apiEnv, 7100, 199)
	r := e.call(t, http.MethodPost, "/v1/users/by-token", map[string]any{"sub_token": u.SubToken})
	mustStatus(t, r, http.StatusOK)
	var got struct {
		User         map[string]any `json:"user"`
		Keys         []any          `json:"keys"`
		Subscription any            `json:"subscription"`
		Plan         any            `json:"plan"`
	}
	r.decode(t, &got)
	if got.User == nil || len(got.Keys) != 1 || got.Subscription != nil || got.Plan != nil {
		t.Fatalf("by-token = %s", r.body)
	}
	mustStatus(t, e.call(t, http.MethodPost, "/v1/users/by-token", map[string]any{"sub_token": "nope"}), http.StatusNotFound)
	mustStatus(t, e.call(t, http.MethodPost, "/v1/users/by-token", map[string]any{"sub_token": u.SubToken, "x": 1}), http.StatusBadRequest)
}
