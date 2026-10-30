package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
	"github.com/ITG-Labs/sylph/services/core/internal/yookassa"
)

type fakeProvider struct {
	mu       sync.Mutex
	requests []yookassa.CreateRequest
	keys     []string
	payments map[string]*yookassa.Payment
	gets     int
	getErr   error
	failIDs  map[string]bool
	delay    time.Duration
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{payments: map[string]*yookassa.Payment{}, failIDs: map[string]bool{}}
}

func (f *fakeProvider) CreatePayment(_ context.Context, req yookassa.CreateRequest, key string) (*yookassa.Payment, error) {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	f.keys = append(f.keys, key)
	id := model.NewUUID()
	meta := map[string]string{}
	for k, v := range req.Metadata {
		meta[k] = v
	}
	p := &yookassa.Payment{ID: id, Status: "pending", Amount: req.Amount, Metadata: meta,
		Confirmation: &yookassa.Confirmation{Type: "redirect", ConfirmationURL: "https://yoomoney.example/checkout?orderId=" + id}}
	f.payments[id] = p
	cp := *p
	return &cp, nil
}

func (f *fakeProvider) GetPayment(_ context.Context, id string) (*yookassa.Payment, error) {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.getErr != nil || f.failIDs[id] {
		return nil, errors.New("connection refused")
	}
	p, ok := f.payments[id]
	if !ok {
		return nil, yookassa.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *fakeProvider) set(id string, mutate func(*yookassa.Payment)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mutate(f.payments[id])
}

func (f *fakeProvider) getCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets
}

func paymentsService(t *testing.T) (*Service, *fakeProvider, *logBuffer) {
	t.Helper()
	svc, logs := setupServiceWithLogs(t)
	fp := newFakeProvider()
	svc.EnablePayments(fp, "https://t.me/sylph_bot")
	return svc, fp, logs
}

func userAndPlan(t *testing.T, svc *Service, tg int64, price int64) (*model.User, *model.Plan) {
	t.Helper()
	ctx := context.Background()
	u, _, err := svc.RegisterUser(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	p := &model.Plan{Name: "Месяц " + strconv.FormatInt(tg, 10), PriceRub: price, PeriodDays: 30, LocationGroupID: testGroup(t, svc.store)}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	return u, p
}

func succeed(fp *fakeProvider, id string) {
	fp.set(id, func(p *yookassa.Payment) { p.Status = "succeeded"; p.Paid = true })
}

func TestCreatePaymentNeedsTheProvider(t *testing.T) {
	svc := setupService(t)
	u, p := userAndPlan(t, svc, 10, 199)
	if _, err := svc.CreatePayment(context.Background(), u.ID, p.ID); !errors.Is(err, ErrPaymentsDisabled) {
		t.Fatalf("err = %v, want ErrPaymentsDisabled", err)
	}
	if _, err := svc.CheckPayment(context.Background(), "x"); !errors.Is(err, ErrPaymentsDisabled) {
		t.Fatalf("check err = %v, want ErrPaymentsDisabled", err)
	}
	if _, err := svc.PollPayments(context.Background()); !errors.Is(err, ErrPaymentsDisabled) {
		t.Fatalf("poll err = %v, want ErrPaymentsDisabled", err)
	}
}

func TestCreatePaymentSendsThePlanToTheProvider(t *testing.T) {
	svc, fp, logs := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 11, 199)
	pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.requests) != 1 {
		t.Fatalf("provider calls = %d", len(fp.requests))
	}
	req := fp.requests[0]
	if req.Amount != yookassa.RUB(199) || !req.Capture || req.Confirmation.Type != "redirect" || req.Confirmation.ReturnURL != "https://t.me/sylph_bot" ||
		req.Description != fmt.Sprintf("%d-11", p.ID) || req.Metadata["user_id"] != strconv.FormatInt(u.ID, 10) || req.Metadata["plan_id"] != strconv.FormatInt(p.ID, 10) {
		t.Fatalf("request = %+v", req)
	}
	if k := fp.keys[0]; k == "" || len(k) > 64 {
		t.Fatalf("idempotence key = %q", k)
	}
	stored, err := svc.store.GetPayment(ctx, pm.PaymentID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "pending" || stored.Source != "yookassa" || stored.AmountRub != 199 || stored.UserID != u.ID || stored.PlanID != p.ID ||
		stored.ConfirmationURL == "" || stored.ConfirmationURL != pm.ConfirmationURL {
		t.Fatalf("stored = %+v", stored)
	}
	if !logs.has("payment_created") {
		t.Fatalf("no payment_created log:\n%s", logs.String())
	}
}

func TestCreatePaymentDescribesPlanAndTelegramID(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, _, _ := svc.RegisterUser(ctx, 7001234567)
	long := ""
	for range 200 {
		long += "я"
	}
	p := &model.Plan{Name: long, PriceRub: 10, PeriodDays: 30, LocationGroupID: testGroup(t, svc.store)}
	if err := svc.store.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePayment(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	if got, want := fp.requests[0].Description, fmt.Sprintf("%d-7001234567", p.ID); got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
}

func TestCreatePaymentReusesAFreshPendingOne(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 13, 199)
	first, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.PaymentID != first.PaymentID || again.ConfirmationURL != first.ConfirmationURL || len(fp.requests) != 1 {
		t.Fatalf("second create made a new payment: %+v vs %+v (%d calls)", again, first, len(fp.requests))
	}
	if _, err := poolOf(svc).Exec(ctx, `UPDATE payments SET created_at = now() - interval '51 minutes'`); err != nil {
		t.Fatal(err)
	}
	stale, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.PaymentID == first.PaymentID || len(fp.requests) != 2 {
		t.Fatal("payment older than 50 minutes was reused")
	}
}

func TestCreatePaymentRefusesPlansItCannotSell(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, free := userAndPlan(t, svc, 14, 0)
	_, hidden := userAndPlan(t, svc, 15, 100)
	hidden.OnSale = false
	if err := svc.store.UpdatePlan(ctx, hidden); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := svc.CreatePayment(ctx, u.ID, free.ID); !errors.As(err, &ce) {
		t.Fatalf("free plan err = %v", err)
	}
	if _, err := svc.CreatePayment(ctx, u.ID, hidden.ID); !errors.As(err, &ce) {
		t.Fatalf("off-sale plan err = %v", err)
	}
	if _, err := svc.CreatePayment(ctx, u.ID, 424242); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown plan err = %v", err)
	}
	if _, err := svc.CreatePayment(ctx, 424242, hidden.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user err = %v", err)
	}
	_, current := userAndPlan(t, svc, 23, 100)
	if err := svc.HandlePayment(ctx, "manual-14", u.ID, current.ID, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePayment(ctx, u.ID, hidden.ID); !errors.As(err, &ce) {
		t.Fatalf("off-sale plan while on another plan err = %v", err)
	}
	if len(fp.requests) != 0 {
		t.Fatalf("provider called %d times for refused payments", len(fp.requests))
	}
}

func TestCreatePaymentRenewsTheCurrentOffSalePlan(t *testing.T) {
	svc, _, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 16, 100)
	if err := svc.HandlePayment(ctx, "manual-16", u.ID, p.ID, 100); err != nil {
		t.Fatal(err)
	}
	p.OnSale = false
	if err := svc.store.UpdatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePayment(ctx, u.ID, p.ID); err != nil {
		t.Fatalf("renewal of the current off-sale plan refused: %v", err)
	}
}

func TestCheckPaymentAppliesASucceededPaymentOnce(t *testing.T) {
	svc, fp, logs := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 17, 199)
	pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.CheckPayment(ctx, pm.PaymentID)
	if err != nil || pending.Status != "pending" || pending.Applied {
		t.Fatalf("check before paying = %+v, %v", pending, err)
	}
	succeed(fp, pm.PaymentID)
	res, err := svc.CheckPayment(ctx, pm.PaymentID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "succeeded" || !res.Applied || res.TelegramID == nil || *res.TelegramID != 17 || res.PaidUntil == nil {
		t.Fatalf("check = %+v", res)
	}
	gets := fp.getCount()
	again, err := svc.CheckPayment(ctx, pm.PaymentID)
	if err != nil || again.Status != "succeeded" || again.Applied || again.PaidUntil == nil {
		t.Fatalf("second check = %+v, %v", again, err)
	}
	if fp.getCount() != gets {
		t.Fatal("final payment checked with the provider again")
	}
	sub, err := svc.store.GetActiveSubscription(ctx, u.ID)
	if err != nil || sub.PlanID != p.ID {
		t.Fatalf("subscription = %+v, %v", sub, err)
	}
	if !logs.has("payment_applied") {
		t.Fatalf("no payment_applied log:\n%s", logs.String())
	}
}

func TestCheckPaymentRejectsWhatDoesNotMatch(t *testing.T) {
	cases := map[string]func(*yookassa.Payment){
		"amount":   func(p *yookassa.Payment) { p.Amount.Value = "1.00" },
		"kopecks":  func(p *yookassa.Payment) { p.Amount.Value = "199.01" },
		"currency": func(p *yookassa.Payment) { p.Amount.Currency = "USD" },
		"garbage":  func(p *yookassa.Payment) { p.Amount.Value = "199" },
		"user":     func(p *yookassa.Payment) { p.Metadata["user_id"] = "999" },
		"plan":     func(p *yookassa.Payment) { p.Metadata["plan_id"] = "999" },
		"no meta":  func(p *yookassa.Payment) { p.Metadata = nil },
		"id":       func(p *yookassa.Payment) { p.ID = model.NewUUID() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			svc, fp, logs := paymentsService(t)
			ctx := context.Background()
			u, p := userAndPlan(t, svc, 18, 199)
			pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			fp.set(pm.PaymentID, func(y *yookassa.Payment) { y.Status = "succeeded"; mutate(y) })
			res, err := svc.CheckPayment(ctx, pm.PaymentID)
			if err != nil || res.Status != "mismatch" || res.Applied {
				t.Fatalf("check = %+v, %v", res, err)
			}
			if _, err := svc.store.GetActiveSubscription(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("mismatched payment created a period: %v", err)
			}
			if !logs.has("payment_mismatch") {
				t.Fatalf("no payment_mismatch log:\n%s", logs.String())
			}
		})
	}
}

func TestCheckPaymentMarksCanceled(t *testing.T) {
	svc, fp, logs := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 19, 199)
	pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	fp.set(pm.PaymentID, func(y *yookassa.Payment) {
		y.Status = "canceled"
		y.CancellationDetails = &yookassa.CancellationDetails{Party: "yoo_money", Reason: "expired_on_confirmation"}
	})
	res, err := svc.CheckPayment(ctx, pm.PaymentID)
	if err != nil || res.Status != "canceled" || res.Applied {
		t.Fatalf("check = %+v, %v", res, err)
	}
	if got, _ := svc.store.GetPayment(ctx, pm.PaymentID); got.Status != "canceled" {
		t.Fatalf("stored status = %q", got.Status)
	}
	if !logs.has("payment_canceled") {
		t.Fatalf("no payment_canceled log:\n%s", logs.String())
	}
}

func TestCheckPaymentKeepsWaitingPaymentsPending(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 20, 199)
	pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	fp.set(pm.PaymentID, func(y *yookassa.Payment) { y.Status = "waiting_for_capture" })
	res, err := svc.CheckPayment(ctx, pm.PaymentID)
	if err != nil || res.Status != "pending" || res.Applied {
		t.Fatalf("check = %+v, %v", res, err)
	}
	if got, _ := svc.store.GetPayment(ctx, pm.PaymentID); got.Status != "pending" {
		t.Fatalf("stored status = %q", got.Status)
	}
}

func TestCheckPaymentWhenTheProviderIsDown(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 21, 199)
	pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	fp.getErr = errors.New("down")
	if _, err := svc.CheckPayment(ctx, pm.PaymentID); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
	if got, _ := svc.store.GetPayment(ctx, pm.PaymentID); got.Status != "pending" {
		t.Fatalf("stored status = %q", got.Status)
	}
}

func TestCheckPaymentOnlyForProviderPaymentsWeCreated(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 22, 199)
	if _, err := svc.CheckPayment(ctx, model.NewUUID()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown payment err = %v", err)
	}
	if err := svc.store.CreatePayment(ctx, &model.Payment{PaymentID: "manual-22", UserID: u.ID, PlanID: p.ID, AmountRub: 199, Status: "pending", Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CheckPayment(ctx, "manual-22"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("manual payment err = %v", err)
	}
	if fp.getCount() != 0 {
		t.Fatalf("provider asked about payments we did not create: %d", fp.getCount())
	}
}

func TestPollPaymentsSettlesWhatTheProviderKnows(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	var ids []string
	for i, tg := range []int64{30, 31, 32, 33, 34} {
		u, p := userAndPlan(t, svc, tg, int64(100+i))
		pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, pm.PaymentID)
	}
	succeed(fp, ids[0])
	fp.set(ids[1], func(y *yookassa.Payment) { y.Status = "canceled" })
	fp.set(ids[3], func(y *yookassa.Payment) { y.Status = "succeeded"; y.Amount.Value = "1.00" })
	fp.failIDs[ids[4]] = true
	res, err := svc.PollPayments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res != (PollResult{Checked: 5, Succeeded: 1, Canceled: 1, Mismatch: 1, Failed: 1}) {
		t.Fatalf("poll = %+v", res)
	}
	again, err := svc.PollPayments(ctx)
	if err != nil || again.Checked != 2 {
		t.Fatalf("second poll = %+v, %v; want only the two still pending", again, err)
	}
}

func TestCheckAndPollRacingApplyOnce(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 40, 199)
	pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	succeed(fp, pm.PaymentID)
	var wg sync.WaitGroup
	var mu sync.Mutex
	applied := 0
	for range 6 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			res, err := svc.CheckPayment(ctx, pm.PaymentID)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if res.Applied {
				applied++
			}
		}()
		go func() {
			defer wg.Done()
			res, err := svc.PollPayments(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			applied += res.Succeeded
		}()
	}
	wg.Wait()
	if applied != 1 {
		t.Fatalf("applied %d times", applied)
	}
	var periods int
	if err := poolOf(svc).QueryRow(ctx, `SELECT count(*) FROM subscriptions WHERE user_id = $1`, u.ID).Scan(&periods); err != nil || periods != 1 {
		t.Fatalf("periods = %d, %v", periods, err)
	}
}

func TestPaidUntilComesFromTheQueue(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 41, 199)
	for range 2 {
		pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		succeed(fp, pm.PaymentID)
		res, err := svc.CheckPayment(ctx, pm.PaymentID)
		if err != nil || !res.Applied {
			t.Fatalf("check = %+v, %v", res, err)
		}
		if res.PaidUntil == nil || res.PaidUntil.Before(time.Now().Add(29*24*time.Hour)) {
			t.Fatalf("paid until = %v", res.PaidUntil)
		}
	}
	sub, _ := svc.store.GetActiveSubscription(ctx, u.ID)
	if sub.PaidUntil == nil || sub.PaidUntil.Sub(sub.ExpiresAt) < 29*24*time.Hour {
		t.Fatalf("second payment did not queue: %+v", sub)
	}
}

func TestConcurrentCreatesMakeOnePayment(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	fp.delay = 50 * time.Millisecond
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 50, 199)
	var wg sync.WaitGroup
	ids := make([]string, 6)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = pm.PaymentID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("payment ids = %v", ids)
		}
	}
	if len(fp.requests) != 1 {
		t.Fatalf("provider asked to create %d payments", len(fp.requests))
	}
}

func TestReuseChecksThatTheOldLinkIsStillAlive(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 51, 199)
	first, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	fp.set(first.PaymentID, func(y *yookassa.Payment) { y.Status = "canceled" })
	second, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.PaymentID == first.PaymentID || len(fp.requests) != 2 {
		t.Fatalf("canceled link handed out again: %+v", second)
	}
	if got, _ := svc.store.GetPayment(ctx, first.PaymentID); got.Status != "canceled" {
		t.Fatalf("old payment = %q", got.Status)
	}
	fp.getErr = errors.New("down")
	third, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil || third.PaymentID != second.PaymentID {
		t.Fatalf("provider down must not block the fresh link: %+v, %v", third, err)
	}
}

func TestReusePaidLinkSettlesItAndMakesANewOne(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 52, 199)
	first, _ := svc.CreatePayment(ctx, u.ID, p.ID)
	succeed(fp, first.PaymentID)
	second, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil || second.PaymentID == first.PaymentID {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if got, _ := svc.store.GetPayment(ctx, first.PaymentID); got.Status != "succeeded" {
		t.Fatalf("paid payment found on reuse was not settled: %q", got.Status)
	}
}

func TestPollStopsWithinItsTimeBudgetNewestFirst(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	var ids []string
	for i, tg := range []int64{60, 61, 62, 63, 64} {
		u, p := userAndPlan(t, svc, tg, int64(100+i))
		pm, err := svc.CreatePayment(ctx, u.ID, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, pm.PaymentID)
		if _, err := poolOf(svc).Exec(ctx, `UPDATE payments SET created_at = now() - make_interval(mins => $2) WHERE payment_id = $1`, pm.PaymentID, 10-i); err != nil {
			t.Fatal(err)
		}
	}
	succeed(fp, ids[4])
	fp.delay = 40 * time.Millisecond
	svc.pollBudget = 60 * time.Millisecond
	res, err := svc.PollPayments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Checked == 0 || res.Checked >= 5 || res.Succeeded != 1 {
		t.Fatalf("poll = %+v; want a partial run that started from the newest payment", res)
	}
}
