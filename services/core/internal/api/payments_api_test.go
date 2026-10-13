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

type createdPayment struct {
	PaymentID       string `json:"payment_id"`
	ConfirmationURL string `json:"confirmation_url"`
	AmountRub       int64  `json:"amount_rub"`
}

type checkedPayment struct {
	Status     string  `json:"status"`
	Applied    bool    `json:"applied"`
	TelegramID *int64  `json:"telegram_id"`
	PaidUntil  *string `json:"paid_until"`
}

func botUserAndPlan(t *testing.T, e *apiEnv, tg int64, price int64) (*model.User, *model.Plan) {
	t.Helper()
	ctx := context.Background()
	u, _, err := e.svc.RegisterUser(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	p := &model.Plan{Name: fmt.Sprintf("Месяц %d", tg), PriceRub: price, PeriodDays: 30}
	if err := e.svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	return u, p
}

func TestPaymentCreateAndCheck(t *testing.T) {
	e, yk := newPaymentsEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	u, p := botUserAndPlan(t, e, 5100, 199)
	var created createdPayment
	r := adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": p.ID})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &created)
	if created.PaymentID == "" || !strings.HasPrefix(created.ConfirmationURL, "https://yoomoney.example/") || created.AmountRub != 199 {
		t.Fatalf("created = %+v", created)
	}
	check := func() checkedPayment {
		t.Helper()
		r := adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/"+created.PaymentID+"/check", nil)
		mustStatus(t, r, http.StatusOK)
		var c checkedPayment
		r.decode(t, &c)
		return c
	}
	if c := check(); c.Status != "pending" || c.Applied {
		t.Fatalf("check before paying = %+v", c)
	}
	yk.setStatus(created.PaymentID, "succeeded")
	c := check()
	if c.Status != "succeeded" || !c.Applied || c.TelegramID == nil || *c.TelegramID != 5100 || c.PaidUntil == nil {
		t.Fatalf("check after paying = %+v", c)
	}
	if again := check(); again.Status != "succeeded" || again.Applied {
		t.Fatalf("second check = %+v", again)
	}
	var status map[string]any
	r = adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/users/5100", nil)
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &status)
	if status["subscription"] == nil {
		t.Fatalf("no subscription after payment: %v", status)
	}
}

func TestPaymentCreateReusesThePendingLink(t *testing.T) {
	e, yk := newPaymentsEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	u, p := botUserAndPlan(t, e, 5101, 199)
	var a, b createdPayment
	adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": p.ID}).decode(t, &a)
	adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": p.ID}).decode(t, &b)
	if a.PaymentID == "" || a != b || yk.createCount() != 1 {
		t.Fatalf("second create = %+v, first = %+v, provider creates = %d", b, a, yk.createCount())
	}
}

func TestPaymentCreateErrors(t *testing.T) {
	e, _ := newPaymentsEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	panel := e.client(t, "panel", mtls.RolePanel)
	u, p := botUserAndPlan(t, e, 5102, 199)
	_, free := botUserAndPlan(t, e, 5103, 0)
	url := e.ts.URL + "/v1/payments/create"
	mustStatus(t, adminCall(t, bot, http.MethodPost, url, map[string]any{"user_id": u.ID}), http.StatusBadRequest)
	mustStatus(t, adminCall(t, bot, http.MethodPost, url, map[string]any{"user_id": u.ID, "plan_id": p.ID, "amount_rub": 1}), http.StatusBadRequest)
	mustStatus(t, adminCall(t, bot, http.MethodPost, url, map[string]any{"user_id": u.ID, "plan_id": 424242}), http.StatusNotFound)
	mustStatus(t, adminCall(t, bot, http.MethodPost, url, map[string]any{"user_id": 424242, "plan_id": p.ID}), http.StatusNotFound)
	mustField(t, adminCall(t, bot, http.MethodPost, url, map[string]any{"user_id": u.ID, "plan_id": free.ID}), http.StatusConflict, "conflict", "plan_id")
	mustStatus(t, adminCall(t, panel, http.MethodPatch, fmt.Sprintf("%s/v1/admin/plans/%d", e.ts.URL, p.ID), map[string]any{"on_sale": false}), http.StatusOK)
	mustField(t, adminCall(t, bot, http.MethodPost, url, map[string]any{"user_id": u.ID, "plan_id": p.ID}), http.StatusConflict, "conflict", "plan_id")
}

func TestPaymentCheckErrors(t *testing.T) {
	e, yk := newPaymentsEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	u, p := botUserAndPlan(t, e, 5104, 199)
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/"+model.NewUUID()+"/check", nil), http.StatusNotFound)
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/"+strings.Repeat("a", 65)+"/check", nil), http.StatusBadRequest)
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/bad%20id/check", nil), http.StatusBadRequest)
	var created createdPayment
	adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": p.ID}).decode(t, &created)
	yk.setDown(true)
	mustField(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/"+created.PaymentID+"/check", nil), http.StatusBadGateway, "provider_unavailable", "")
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": p.ID}), http.StatusOK)
}

func TestPaymentsDisabledWithoutProvider(t *testing.T) {
	e := newAPIEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	cron := e.client(t, "cron", mtls.RoleCron)
	u, p := botUserAndPlan(t, e, 5105, 199)
	mustField(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": p.ID}), http.StatusServiceUnavailable, "payments_disabled", "")
	mustField(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/"+model.NewUUID()+"/check", nil), http.StatusServiceUnavailable, "payments_disabled", "")
	mustField(t, adminCall(t, cron, http.MethodPost, e.ts.URL+"/v1/maintenance/payments/poll", nil), http.StatusServiceUnavailable, "payments_disabled", "")
}

func TestPaymentsPollEndpoint(t *testing.T) {
	e, yk := newPaymentsEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	cron := e.client(t, "cron", mtls.RoleCron)
	u, p := botUserAndPlan(t, e, 5106, 199)
	var created createdPayment
	adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": p.ID}).decode(t, &created)
	yk.setStatus(created.PaymentID, "succeeded")
	r := adminCall(t, cron, http.MethodPost, e.ts.URL+"/v1/maintenance/payments/poll", nil)
	mustStatus(t, r, http.StatusOK)
	var got map[string]int
	r.decode(t, &got)
	if len(got) != 5 || got["checked"] != 1 || got["succeeded"] != 1 {
		t.Fatalf("poll = %v", got)
	}
}

func TestPlansForTheBot(t *testing.T) {
	e := newAPIEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	panel := e.client(t, "panel", mtls.RolePanel)
	_, keep := botUserAndPlan(t, e, 5107, 199)
	_, hide := botUserAndPlan(t, e, 5108, 99)
	r := adminCall(t, panel, http.MethodPatch, fmt.Sprintf("%s/v1/admin/plans/%d", e.ts.URL, hide.ID), map[string]any{"on_sale": false})
	mustStatus(t, r, http.StatusOK)
	var patched model.Plan
	r.decode(t, &patched)
	if patched.OnSale {
		t.Fatalf("patched plan = %+v", patched)
	}
	r = adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/plans", nil)
	mustStatus(t, r, http.StatusOK)
	var list []model.Plan
	r.decode(t, &list)
	if len(list) != 1 || list[0].ID != keep.ID || !list[0].OnSale {
		t.Fatalf("plans = %+v", list)
	}
}

func TestPlansForTheBotIsAnEmptyList(t *testing.T) {
	e := newAPIEnv(t)
	r := adminCall(t, e.client(t, "bot", mtls.RoleBot), http.MethodGet, e.ts.URL+"/v1/plans", nil)
	mustStatus(t, r, http.StatusOK)
	if strings.TrimSpace(string(r.body)) != "[]" {
		t.Fatalf("plans = %s", r.body)
	}
}

func TestUsersCreateIsIdempotent(t *testing.T) {
	e := newAPIEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	var a, b struct {
		ID       int64  `json:"id"`
		SubToken string `json:"sub_token"`
	}
	adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users", map[string]int64{"telegram_id": 5200}).decode(t, &a)
	r := adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users", map[string]int64{"telegram_id": 5200})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &b)
	if a.ID == 0 || a != b {
		t.Fatalf("second create = %+v, first = %+v", b, a)
	}
}

func TestUsersGetWithoutSubscription(t *testing.T) {
	e := newAPIEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	if _, _, err := e.svc.RegisterUser(context.Background(), 5201); err != nil {
		t.Fatal(err)
	}
	r := adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/users/5201", nil)
	mustStatus(t, r, http.StatusOK)
	var m map[string]any
	r.decode(t, &m)
	user, _ := m["user"].(map[string]any)
	if user["sub_token"] == "" || m["subscription"] != nil || m["plan"] != nil || m["devices"] != float64(0) {
		t.Fatalf("status = %v", m)
	}
}

func TestRotateGivesANewLinkAndUnknownUsersAre404(t *testing.T) {
	e := newAPIEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	subs := e.client(t, "subs", mtls.RoleSubs)
	u, oldKey, _ := e.svc.RegisterUser(context.Background(), 5202)
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := e.svc.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.HandlePayment(context.Background(), "manual-5202", u.ID, p.ID, 100); err != nil {
		t.Fatal(err)
	}
	r := adminCall(t, bot, http.MethodPost, fmt.Sprintf("%s/v1/users/%d/rotate", e.ts.URL, u.ID), nil)
	mustStatus(t, r, http.StatusOK)
	var m map[string]any
	r.decode(t, &m)
	token, _ := m["sub_token"].(string)
	if len(m) != 1 || token == "" || token == u.SubToken {
		t.Fatalf("rotate = %v, want only a new sub_token", m)
	}
	mustStatus(t, adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/users/by-token", map[string]string{"sub_token": u.SubToken}), http.StatusNotFound)
	keys := userKeysByToken(t, subs, e.ts.URL, token)
	if len(keys) != 1 || keys[0] == oldKey {
		t.Fatalf("keys after rotate = %v", keys)
	}
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users/424242/rotate", nil), http.StatusNotFound)
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users/424242/devices/reset", nil), http.StatusNotFound)
}

func TestManualPaymentAnswersHonestly(t *testing.T) {
	e, _ := newPaymentsEnv(t)
	panel := e.client(t, "panel", mtls.RolePanel)
	bot := e.client(t, "bot", mtls.RoleBot)
	u, p := botUserAndPlan(t, e, 5300, 199)
	other, _ := botUserAndPlan(t, e, 5301, 199)
	body := map[string]any{"payment_id": "manual-5300", "user_id": u.ID, "plan_id": p.ID, "amount_rub": 199}
	var first, again map[string]any
	r := adminCall(t, panel, http.MethodPost, e.ts.URL+"/v1/payments", body)
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &first)
	adminCall(t, panel, http.MethodPost, e.ts.URL+"/v1/payments", body).decode(t, &again)
	if first["applied"] != true || first["status"] != "succeeded" || again["applied"] != false || again["status"] != "succeeded" {
		t.Fatalf("first = %v, again = %v", first, again)
	}
	mustField(t, adminCall(t, panel, http.MethodPost, e.ts.URL+"/v1/payments", map[string]any{"payment_id": "manual-5300", "user_id": other.ID, "plan_id": p.ID, "amount_rub": 199}), http.StatusConflict, "conflict", "payment_id")
	var created createdPayment
	adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/payments/create", map[string]any{"user_id": u.ID, "plan_id": p.ID}).decode(t, &created)
	mustField(t, adminCall(t, panel, http.MethodPost, e.ts.URL+"/v1/payments", map[string]any{"payment_id": created.PaymentID, "user_id": u.ID, "plan_id": p.ID, "amount_rub": 199}), http.StatusConflict, "conflict", "payment_id")
}
