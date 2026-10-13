package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestApplyMismatchCreatesTheLinkedPeriod(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	pp := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.ApplyMismatchPayment(ctx, pp.PaymentID, "ok", time.Now().UTC()); !errors.Is(err, ErrPaymentState) {
		t.Fatalf("apply of a pending payment: %v", err)
	}
	if _, err := s.FinishPayment(ctx, pp.PaymentID, "mismatch"); err != nil {
		t.Fatal(err)
	}
	res, err := s.ApplyMismatchPayment(ctx, pp.PaymentID, "amount checked by hand", time.Now().UTC())
	if err != nil || !res.Applied {
		t.Fatalf("apply: %+v, %v", res, err)
	}
	got := paymentRow(t, s, pp.PaymentID)
	if got.Status != "succeeded" || got.PaidAt == nil || got.PeriodID == nil || *got.PeriodID != res.Period.PeriodID || got.Note == nil || *got.Note != "amount checked by hand" {
		t.Fatalf("payment after apply: %+v", got)
	}
	if _, err := s.GetActiveSubscription(ctx, u.ID); err != nil {
		t.Fatalf("no period after apply: %v", err)
	}
	if _, err := s.ApplyMismatchPayment(ctx, pp.PaymentID, "again", time.Now().UTC()); !errors.Is(err, ErrPaymentState) {
		t.Fatalf("second apply: %v", err)
	}
	if _, err := s.ApplyMismatchPayment(ctx, "nope", "x", time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown payment: %v", err)
	}
}

func TestApplyMismatchRefusesABannedUser(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	pp := mkProviderPayment(t, s, u.ID, mkPlan(t, s).ID)
	if _, err := s.FinishPayment(ctx, pp.PaymentID, "mismatch"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BanUser(ctx, u.ID, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyMismatchPayment(ctx, pp.PaymentID, "x", time.Now().UTC()); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("apply for a banned user: %v", err)
	}
	if got := paymentRow(t, s, pp.PaymentID); got.Status != "mismatch" {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestRejectPaymentOnlyFromAllowedStatuses(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	pp := mkProviderPayment(t, s, u.ID, p.ID)
	if err := s.RejectPayment(ctx, pp.PaymentID, "x", "mismatch"); !errors.Is(err, ErrPaymentState) {
		t.Fatalf("reject of a pending payment as mismatch: %v", err)
	}
	if err := s.RejectPayment(ctx, pp.PaymentID, "unknown to the shop", "pending", "mismatch"); err != nil {
		t.Fatal(err)
	}
	got := paymentRow(t, s, pp.PaymentID)
	if got.Status != "rejected" || got.Note == nil || *got.Note != "unknown to the shop" {
		t.Fatalf("after reject: %+v", got)
	}
	if _, err := s.SettlePayment(ctx, pp.PaymentID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got := paymentRow(t, s, pp.PaymentID); got.Status != "rejected" {
		t.Fatal("a rejected payment was settled later")
	}
	if err := s.RejectPayment(ctx, "nope", "x", "pending"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown payment: %v", err)
	}
}

func TestRefundClosesThePeriodOfThePayment(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	now := time.Now().UTC()
	pay(t, s, u.ID, p.ID, now)
	queued := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.SettlePayment(ctx, queued.PaymentID, now); err != nil {
		t.Fatal(err)
	}
	pay(t, s, u.ID, p.ID, now)
	if _, err := s.RefundPayment(ctx, "nope", "x", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown payment: %v", err)
	}
	before := userPeriods(t, s, u.ID)
	closed, err := s.RefundPayment(ctx, queued.PaymentID, "asked for it", now)
	if err != nil || !closed {
		t.Fatalf("refund: closed %v, %v", closed, err)
	}
	got := paymentRow(t, s, queued.PaymentID)
	if got.Status != "refunded" || got.RefundedAt == nil || got.Note == nil {
		t.Fatalf("payment after refund: %+v", got)
	}
	after := userPeriods(t, s, u.ID)
	if !after[1].Canceled || after[0].Canceled || after[2].Canceled {
		t.Fatalf("periods after refund: %+v", after)
	}
	if !sameTime(after[2].StartsAt, before[0].ExpiresAt) {
		t.Fatalf("last period did not move up: starts %v, want %v", after[2].StartsAt, before[0].ExpiresAt)
	}
	if _, err := s.RefundPayment(ctx, queued.PaymentID, "again", now); !errors.Is(err, ErrPaymentState) {
		t.Fatalf("second refund: %v", err)
	}
}

func TestRefundOfAnOldPaymentOnlyMarksIt(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	pp := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.SettlePayment(ctx, pp.PaymentID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE payments SET period_id = NULL WHERE payment_id = $1`, pp.PaymentID); err != nil {
		t.Fatal(err)
	}
	closed, err := s.RefundPayment(ctx, pp.PaymentID, "legacy", time.Now().UTC())
	if err != nil || closed {
		t.Fatalf("legacy refund: closed %v, %v", closed, err)
	}
	if _, err := s.GetActiveSubscription(ctx, u.ID); err != nil {
		t.Fatal("legacy refund touched a period it does not know")
	}
	other := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.FinishPayment(ctx, other.PaymentID, "canceled"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RefundPayment(ctx, other.PaymentID, "x", time.Now().UTC()); !errors.Is(err, ErrPaymentState) {
		t.Fatalf("refund of a canceled payment: %v", err)
	}
}
