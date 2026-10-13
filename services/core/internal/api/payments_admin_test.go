package api

import (
	"net/http"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func TestPaymentsInThePanel(t *testing.T) {
	e, yk := newPaymentsEnv(t)
	panel := e.client(t, "panel", mtls.RolePanel)
	call := func(method, path string, body any) adminResp {
		t.Helper()
		return adminCall(t, panel, method, e.ts.URL+path, body)
	}
	u, p := botUserAndPlan(t, e, 9100, 199)
	var created createdPayment
	r := call(http.MethodPost, "/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": p.ID})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &created)
	base := "/v1/admin/payments/" + created.PaymentID

	r = call(http.MethodGet, "/v1/admin/payments?status=pending", nil)
	mustStatus(t, r, http.StatusOK)
	var page struct {
		Payments []struct {
			PaymentID  string `json:"payment_id"`
			TelegramID int64  `json:"telegram_id"`
			PlanName   string `json:"plan_name"`
		} `json:"payments"`
		NextBefore *int64 `json:"next_before"`
	}
	r.decode(t, &page)
	if len(page.Payments) != 1 || page.Payments[0].PaymentID != created.PaymentID || page.Payments[0].TelegramID != 9100 || page.Payments[0].PlanName != p.Name {
		t.Fatalf("list = %s", r.body)
	}
	mustField(t, call(http.MethodGet, "/v1/admin/payments?status=lost", nil), http.StatusBadRequest, "bad_request", "status")
	mustField(t, call(http.MethodGet, "/v1/admin/payments?stuck=maybe", nil), http.StatusBadRequest, "bad_request", "stuck")
	mustStatus(t, call(http.MethodGet, base, nil), http.StatusOK)
	mustStatus(t, call(http.MethodGet, "/v1/admin/payments/nope", nil), http.StatusNotFound)
	mustField(t, call(http.MethodGet, "/v1/admin/payments/bad%20id", nil), http.StatusBadRequest, "bad_request", "payment_id")

	mustField(t, call(http.MethodPost, base+"/reject", map[string]any{"reason": "stuck"}), http.StatusConflict, "conflict", "status")
	mustField(t, call(http.MethodPost, base+"/refund", map[string]any{"reason": "x"}), http.StatusConflict, "conflict", "status")
	mustField(t, call(http.MethodPost, base+"/refund", map[string]any{}), http.StatusBadRequest, "bad_request", "reason")

	yk.setStatus(created.PaymentID, "succeeded")
	r = call(http.MethodPost, base+"/recheck", nil)
	mustStatus(t, r, http.StatusOK)
	var res struct {
		Status       string `json:"status"`
		Applied      bool   `json:"applied"`
		PeriodClosed bool   `json:"period_closed"`
		Provider     struct {
			Found  bool   `json:"found"`
			Status string `json:"status"`
		} `json:"provider"`
	}
	r.decode(t, &res)
	if res.Status != "succeeded" || !res.Applied || !res.Provider.Found || res.Provider.Status != "succeeded" {
		t.Fatalf("recheck = %s", r.body)
	}
	r = call(http.MethodPost, base+"/refund", map[string]any{"reason": "returned in the cabinet"})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &res)
	if res.Status != "refunded" || !res.PeriodClosed {
		t.Fatalf("refund = %s", r.body)
	}
	mustField(t, call(http.MethodPost, base+"/apply", map[string]any{"reason": "x"}), http.StatusConflict, "conflict", "status")

	yk.setDown(true)
	r = call(http.MethodPost, base+"/recheck", nil)
	mustCode(t, r, http.StatusBadGateway, "provider_unavailable")
}

func TestManualPaymentsCannotBeRechecked(t *testing.T) {
	e, _ := newPaymentsEnv(t)
	panel := e.client(t, "panel", mtls.RolePanel)
	u, p := botUserAndPlan(t, e, 9101, 199)
	mustStatus(t, adminCall(t, panel, http.MethodPost, e.ts.URL+"/v1/payments", map[string]any{"payment_id": "cash-1", "user_id": u.ID, "plan_id": p.ID, "amount_rub": 199}), http.StatusOK)
	mustField(t, adminCall(t, panel, http.MethodPost, e.ts.URL+"/v1/admin/payments/cash-1/recheck", nil), http.StatusConflict, "conflict", "source")
	mustStatus(t, adminCall(t, panel, http.MethodPost, e.ts.URL+"/v1/admin/payments/cash-1/refund", map[string]any{"reason": "cash returned"}), http.StatusOK)
}

func TestOverviewAndNodeStatusInTheAPI(t *testing.T) {
	e := newAdminEnv(t)
	e.createNode(t, "node-a", nil)
	r := e.call(t, http.MethodGet, "/v1/admin/overview", nil)
	mustStatus(t, r, http.StatusOK)
	var ov struct {
		Nodes struct {
			Total    int            `json:"total"`
			ByStatus map[string]int `json:"by_status"`
		} `json:"nodes"`
		Users     map[string]int64 `json:"users"`
		Revenue   map[string]any   `json:"revenue"`
		Attention map[string]int64 `json:"attention"`
	}
	r.decode(t, &ov)
	if ov.Nodes.Total != 1 || ov.Nodes.ByStatus["never"] != 1 || ov.Users == nil || ov.Revenue["yookassa"] == nil || ov.Attention == nil {
		t.Fatalf("overview = %s", r.body)
	}
	r = e.call(t, http.MethodGet, "/v1/admin/nodes", nil)
	var nodes []struct {
		Status string `json:"status"`
	}
	r.decode(t, &nodes)
	if len(nodes) != 1 || nodes[0].Status != "never" {
		t.Fatalf("nodes = %s", r.body)
	}
}
