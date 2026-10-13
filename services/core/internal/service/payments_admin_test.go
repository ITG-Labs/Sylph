package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
	"github.com/ITG-Labs/sylph/services/core/internal/yookassa"
)

func agePayment(t *testing.T, svc *Service, paymentID, age string) {
	t.Helper()
	if _, err := poolOf(svc).Exec(context.Background(), `UPDATE payments SET created_at = now() - $2::interval WHERE payment_id = $1`, paymentID, age); err != nil {
		t.Fatal(err)
	}
}

func TestRecheckSettlesAnOldPendingPayment(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 601, 199)
	pay, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	agePayment(t, svc, pay.PaymentID, "3 days")
	succeed(fp, pay.PaymentID)
	res, err := svc.RecheckPayment(ctx, pay.PaymentID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "succeeded" || !res.Applied || res.Provider == nil || !res.Provider.Found || res.Provider.Status != "succeeded" || res.Provider.Amount != "199.00" {
		t.Fatalf("recheck = %+v / %+v", res, res.Provider)
	}
	fp.set(pay.PaymentID, func(p *yookassa.Payment) { p.RefundedAmount = &yookassa.Amount{Value: "199.00", Currency: "RUB"} })
	res, err = svc.RecheckPayment(ctx, pay.PaymentID)
	if err != nil || res.Applied || res.Status != "succeeded" || res.Provider.Refunded != "199.00" {
		t.Fatalf("recheck of a refunded payment = %+v / %+v, %v", res, res.Provider, err)
	}
}

func TestRecheckReportsUnknownPaymentsAndRefusesManualOnes(t *testing.T) {
	svc, _, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 602, 199)
	ghost := &model.Payment{PaymentID: "2d85d5ad-000f-5000-9000-1c4c6dd1d9e6", UserID: u.ID, PlanID: p.ID, AmountRub: 199, Status: "pending", Source: "yookassa"}
	if err := svc.store.CreatePayment(ctx, ghost); err != nil {
		t.Fatal(err)
	}
	res, err := svc.RecheckPayment(ctx, ghost.PaymentID)
	if err != nil || res.Status != "pending" || res.Provider == nil || res.Provider.Found {
		t.Fatalf("recheck of a payment the shop does not know = %+v, %v", res, err)
	}
	if _, err := svc.RecordPayment(ctx, "manual-1", u.ID, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := svc.RecheckPayment(ctx, "manual-1"); !errors.As(err, &ce) || ce.Field != "source" {
		t.Fatalf("recheck of a manual payment: %v", err)
	}
	if _, err := svc.RecheckPayment(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown payment: %v", err)
	}
}

func TestRejectAskTheShopBeforeRejectingPending(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 603, 199)
	fresh, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := svc.RejectPayment(ctx, fresh.PaymentID, "stuck"); !errors.As(err, &ce) || ce.Field != "status" {
		t.Fatalf("reject of a fresh pending payment: %v", err)
	}
	agePayment(t, svc, fresh.PaymentID, "2 days")
	res, err := svc.RejectPayment(ctx, fresh.PaymentID, "stuck for two days")
	if err != nil || res.Status != "rejected" {
		t.Fatalf("reject of an old pending payment = %+v, %v", res, err)
	}

	paid, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	agePayment(t, svc, paid.PaymentID, "2 days")
	succeed(fp, paid.PaymentID)
	res, err = svc.RejectPayment(ctx, paid.PaymentID, "looks stuck")
	if err != nil || res.Status != "succeeded" || !res.Applied {
		t.Fatalf("reject of a payment the shop says is paid = %+v, %v", res, err)
	}

	ghost := &model.Payment{PaymentID: "2d85d5ad-000f-5000-9000-1c4c6dd1d9e7", UserID: u.ID, PlanID: p.ID, AmountRub: 199, Status: "pending", Source: "yookassa"}
	if err := svc.store.CreatePayment(ctx, ghost); err != nil {
		t.Fatal(err)
	}
	if res, err := svc.RejectPayment(ctx, ghost.PaymentID, "not in the shop"); err != nil || res.Status != "rejected" {
		t.Fatalf("reject of a payment unknown to the shop = %+v, %v", res, err)
	}
	fp.getErr = errors.New("down")
	other := &model.Payment{PaymentID: "2d85d5ad-000f-5000-9000-1c4c6dd1d9e8", UserID: u.ID, PlanID: p.ID, AmountRub: 199, Status: "pending", Source: "yookassa"}
	if err := svc.store.CreatePayment(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RejectPayment(ctx, other.PaymentID, "x"); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("reject while the shop is down: %v", err)
	}
}

func TestMismatchCanBeAppliedOrRejected(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 604, 199)
	mkMismatch := func() string {
		pay, err := svc.CreatePayment(ctx, u.ID, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		fp.set(pay.PaymentID, func(x *yookassa.Payment) { x.Status = "succeeded"; x.Amount = yookassa.RUB(1) })
		if res, err := svc.CheckPayment(ctx, pay.PaymentID); err != nil || res.Status != "mismatch" {
			t.Fatalf("mismatch setup = %+v, %v", res, err)
		}
		return pay.PaymentID
	}
	a := mkMismatch()
	var fe *FieldError
	if _, err := svc.ApplyPayment(ctx, a, " "); !errors.As(err, &fe) || fe.Field != "reason" {
		t.Fatalf("apply without a reason: %v", err)
	}
	res, err := svc.ApplyPayment(ctx, a, "paid 1 rub by mistake, gift")
	if err != nil || res.Status != "succeeded" || !res.Applied || res.PeriodID == nil {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	var ce *ConflictError
	if _, err := svc.ApplyPayment(ctx, a, "again"); !errors.As(err, &ce) || ce.Field != "status" {
		t.Fatalf("second apply: %v", err)
	}
	b := mkMismatch()
	if res, err := svc.RejectPayment(ctx, b, "wrong amount"); err != nil || res.Status != "rejected" {
		t.Fatalf("reject of a mismatch = %+v, %v", res, err)
	}
	if _, err := svc.RejectPayment(ctx, a, "x"); !errors.As(err, &ce) || ce.Field != "status" {
		t.Fatalf("reject of a succeeded payment: %v", err)
	}
}

func TestRefundMarksThePaymentAndClosesItsPeriod(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 605, 199)
	pay, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	succeed(fp, pay.PaymentID)
	if _, err := svc.CheckPayment(ctx, pay.PaymentID); err != nil {
		t.Fatal(err)
	}
	var fe *FieldError
	if _, err := svc.RefundPayment(ctx, pay.PaymentID, ""); !errors.As(err, &fe) || fe.Field != "reason" {
		t.Fatalf("refund without a reason: %v", err)
	}
	res, err := svc.RefundPayment(ctx, pay.PaymentID, "returned in the shop cabinet")
	if err != nil || res.Status != "refunded" || !res.PeriodClosed {
		t.Fatalf("refund = %+v, %v", res, err)
	}
	if _, err := svc.store.GetActiveSubscription(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("period survived the refund: %v", err)
	}
	var ce *ConflictError
	if _, err := svc.RefundPayment(ctx, pay.PaymentID, "again"); !errors.As(err, &ce) || ce.Field != "status" {
		t.Fatalf("second refund: %v", err)
	}
}

func TestListPaymentsChecksFilters(t *testing.T) {
	svc, _, _ := paymentsService(t)
	ctx := context.Background()
	var fe *FieldError
	if _, err := svc.ListPayments(ctx, PaymentQuery{Status: "lost", Limit: 10}); !errors.As(err, &fe) || fe.Field != "status" {
		t.Fatalf("bad status: %v", err)
	}
	if _, err := svc.ListPayments(ctx, PaymentQuery{Source: "cash", Limit: 10}); !errors.As(err, &fe) || fe.Field != "source" {
		t.Fatalf("bad source: %v", err)
	}
	u, p := userAndPlan(t, svc, 606, 199)
	if _, err := svc.CreatePayment(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListPayments(ctx, PaymentQuery{Stuck: true, Limit: 10})
	if err != nil || len(rows) != 0 {
		t.Fatalf("fresh pending listed as stuck: %v, %v", rows, err)
	}
}
