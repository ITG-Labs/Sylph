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

func TestPrivatePlansAPI(t *testing.T) {
	e := newAdminEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	var public, vip struct {
		ID int64 `json:"id"`
	}
	e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "month", "price_rub": 199, "period_days": 30}).decode(t, &public)
	e.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "vip", "price_rub": 99, "period_days": 30, "visibility": "private"}).decode(t, &vip)
	var user struct {
		ID int64 `json:"id"`
	}
	r := adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users", map[string]any{"telegram_id": 5550})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &user)
	plansOf := func() []int64 {
		r := adminCall(t, bot, http.MethodGet, fmt.Sprintf("%s/v1/users/%d/plans", e.ts.URL, user.ID), nil)
		mustStatus(t, r, http.StatusOK)
		var list []struct {
			ID int64 `json:"id"`
		}
		r.decode(t, &list)
		ids := []int64{}
		for _, p := range list {
			ids = append(ids, p.ID)
		}
		return ids
	}
	if ids := plansOf(); len(ids) != 1 || ids[0] != public.ID {
		t.Fatalf("plans before invite = %v", ids)
	}
	var catalog []map[string]any
	r = adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/plans", nil)
	r.decode(t, &catalog)
	if len(catalog) != 1 {
		t.Fatalf("public catalog = %v", catalog)
	}
	var inv struct {
		ID      int64  `json:"id"`
		Code    string `json:"code"`
		MaxUses *int   `json:"max_uses"`
	}
	r = e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/plans/%d/invites", vip.ID), map[string]any{"max_uses": 1})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &inv)
	if len(inv.Code) != 22 || inv.MaxUses == nil || *inv.MaxUses != 1 {
		t.Fatalf("invite = %+v", inv)
	}
	if strings.Contains(e.logs.String(), inv.Code) {
		t.Fatal("invite code in logs")
	}
	mustField(t, e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/plans/%d/invites", vip.ID), map[string]any{"max_uses": 0}), http.StatusBadRequest, "bad_request", "max_uses")
	mustField(t, e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/plans/%d/invites", vip.ID), map[string]any{"expires_at": "2001-01-01T00:00:00Z"}), http.StatusBadRequest, "bad_request", "expires_at")
	mustStatus(t, e.call(t, http.MethodPost, "/v1/admin/plans/424242/invites", map[string]any{}), http.StatusNotFound)
	redeem := func(code string) adminResp {
		return adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/invites/redeem", map[string]any{"user_id": user.ID, "code": code})
	}
	r = redeem(inv.Code)
	mustStatus(t, r, http.StatusOK)
	var red struct {
		Plan struct {
			ID int64 `json:"id"`
		} `json:"plan"`
		Already bool `json:"already"`
	}
	r.decode(t, &red)
	if red.Plan.ID != vip.ID || red.Already {
		t.Fatalf("redeem = %+v", red)
	}
	if ids := plansOf(); len(ids) != 2 || ids[0] != vip.ID {
		t.Fatalf("plans after invite = %v", ids)
	}
	mustField(t, redeem("not a code"), http.StatusBadRequest, "bad_request", "code")
	mustStatus(t, redeem("AAAAAAAAAAAAAAAAAAAAAA"), http.StatusNotFound)
	var other struct {
		ID int64 `json:"id"`
	}
	adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users", map[string]any{"telegram_id": 5551}).decode(t, &other)
	r = adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/invites/redeem", map[string]any{"user_id": other.ID, "code": inv.Code})
	if code, _ := r.errorField(t); r.status != http.StatusConflict || code != "invite_used_up" {
		t.Fatalf("used up = %d %s", r.status, r.body)
	}
	var invites []map[string]any
	e.call(t, http.MethodGet, fmt.Sprintf("/v1/admin/plans/%d/invites", vip.ID), nil).decode(t, &invites)
	if len(invites) != 1 || invites[0]["uses"].(float64) != 1 {
		t.Fatalf("invites = %v", invites)
	}
	r = e.call(t, http.MethodDelete, fmt.Sprintf("/v1/admin/invites/%d", inv.ID), nil)
	mustStatus(t, r, http.StatusOK)
	mustStatus(t, e.call(t, http.MethodDelete, "/v1/admin/invites/424242", nil), http.StatusNotFound)
	mustStatus(t, e.call(t, http.MethodPut, fmt.Sprintf("/v1/admin/plans/%d/access/5551", vip.ID), nil), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodPut, fmt.Sprintf("/v1/admin/plans/%d/access/7777", vip.ID), nil), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodPut, fmt.Sprintf("/v1/admin/plans/%d/access/0", vip.ID), nil), http.StatusBadRequest)
	var access []struct {
		TelegramID int64  `json:"telegram_id"`
		Source     string `json:"source"`
	}
	e.call(t, http.MethodGet, fmt.Sprintf("/v1/admin/plans/%d/access", vip.ID), nil).decode(t, &access)
	if len(access) != 3 || access[0].Source != "invite" || access[1].TelegramID != 5551 || access[2].TelegramID != 7777 {
		t.Fatalf("access = %+v", access)
	}
	mustStatus(t, e.call(t, http.MethodDelete, fmt.Sprintf("/v1/admin/plans/%d/access/5550", vip.ID), nil), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodDelete, fmt.Sprintf("/v1/admin/plans/%d/access/5550", vip.ID), nil), http.StatusNotFound)
	if ids := plansOf(); len(ids) != 1 {
		t.Fatalf("plans after revoke = %v", ids)
	}
	var open struct {
		Code string `json:"code"`
	}
	e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/plans/%d/invites", vip.ID), map[string]any{}).decode(t, &open)
	r = redeem(open.Code)
	if code, _ := r.errorField(t); r.status != http.StatusConflict || code != "access_revoked" {
		t.Fatalf("redeem after revoke = %d %s", r.status, r.body)
	}
	if ids := plansOf(); len(ids) != 1 {
		t.Fatalf("plans after a revoked redeem = %v", ids)
	}
	mustStatus(t, adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/users/424242/plans", nil), http.StatusNotFound)
}

func TestPrivatePlanPaymentsThroughTheAPI(t *testing.T) {
	e, yk := newPaymentsEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	panel := e.client(t, "panel", mtls.RolePanel)
	vip := &model.Plan{Name: "vip", PriceRub: 99, PeriodDays: 30, Visibility: model.VisibilityPrivate}
	if err := e.svc.CreatePlan(context.Background(), vip); err != nil {
		t.Fatal(err)
	}
	var u struct {
		ID int64 `json:"id"`
	}
	adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users", map[string]any{"telegram_id": 5200}).decode(t, &u)
	create := func() adminResp {
		return adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": vip.ID})
	}
	mustField(t, create(), http.StatusConflict, "conflict", "plan_id")
	if yk.createCount() != 0 {
		t.Fatal("provider called for a private plan without access")
	}
	mustStatus(t, adminCall(t, panel, http.MethodPut, fmt.Sprintf("%s/v1/admin/plans/%d/access/5200", e.ts.URL, vip.ID), nil), http.StatusOK)
	r := create()
	mustStatus(t, r, http.StatusOK)
	var created createdPayment
	r.decode(t, &created)
	yk.setStatus(created.PaymentID, "succeeded")
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/"+created.PaymentID+"/check", nil), http.StatusOK)
	mustStatus(t, adminCall(t, panel, http.MethodDelete, fmt.Sprintf("%s/v1/admin/plans/%d/access/5200", e.ts.URL, vip.ID), nil), http.StatusOK)
	mustStatus(t, create(), http.StatusOK)
}

func TestRedeemForAnUnknownUserSaysUser(t *testing.T) {
	e := newAdminEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	r := adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/invites/redeem", map[string]any{"user_id": 424242, "code": "AAAAAAAAAAAAAAAAAAAAAA"})
	mustStatus(t, r, http.StatusNotFound)
	if !strings.Contains(string(r.body), "user") {
		t.Fatalf("404 body = %s", r.body)
	}
}
