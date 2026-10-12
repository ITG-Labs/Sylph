package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func setupService(t *testing.T) *Service {
	t.Helper()
	pool := store.TestPoolWithSchema(t, "service_test")
	ctx := context.Background()
	if err := store.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(pool)
	return NewService(st, slog.New(slog.DiscardHandler))
}

type logBuffer struct {
	bytes.Buffer
}

func (l *logBuffer) has(msg string) bool {
	return strings.Contains(l.String(), `"msg":"`+msg+`"`)
}

func setupServiceWithLogs(t *testing.T) (*Service, *logBuffer) {
	t.Helper()
	svc := setupService(t)
	logs := &logBuffer{}
	svc.log = slog.New(slog.NewJSONHandler(logs, nil))
	return svc, logs
}

func TestRegisterUser(t *testing.T) {
	svc := setupService(t)
	u, key, err := svc.RegisterUser(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 || key == "" {
		t.Fatalf("u=%+v key=%q", u, key)
	}
	keys, err := svc.store.GetUserKeys(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].UUID != key {
		t.Fatalf("keys = %+v", keys)
	}
}

func TestHandlePaymentIdempotent(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, _, err := svc.RegisterUser(ctx, 200)
	if err != nil {
		t.Fatal(err)
	}
	p := &model.Plan{Name: "m", PriceRub: 150, PeriodDays: 30}
	if err := svc.store.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandlePayment(ctx, "pay-1", u.ID, p.ID, 150); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandlePayment(ctx, "pay-1", u.ID, p.ID, 150); err != nil {
		t.Fatal(err)
	}
	sub, err := svc.store.GetActiveSubscription(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub.ExpiresAt.Sub(sub.StartsAt) > 31*24*time.Hour {
		t.Fatalf("double renewal: expires %v", sub.ExpiresAt)
	}
}

func TestHandlePaymentUnknownUser(t *testing.T) {
	svc := setupService(t)
	err := svc.HandlePayment(context.Background(), "pay-2", 424242, 1, 100)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPaymentLogTellsWhenThePeriodStarts(t *testing.T) {
	svc, logs := setupServiceWithLogs(t)
	ctx := context.Background()
	u, _, err := svc.RegisterUser(ctx, 120)
	if err != nil {
		t.Fatal(err)
	}
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandlePayment(ctx, "pay-1", u.ID, p.ID, 100); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandlePayment(ctx, "pay-2", u.ID, p.ID, 100); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	if !strings.Contains(out, `"mode":"now"`) || !strings.Contains(out, `"mode":"queued"`) {
		t.Fatalf("payment logs lack the mode:\n%s", out)
	}
	view, err := svc.Subscription(ctx, SubscriptionRequest{SubToken: u.SubToken})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := svc.store.GetActiveSubscription(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.ExpiresAt == nil || !view.ExpiresAt.Equal(*sub.PaidUntil) || !view.ExpiresAt.After(sub.ExpiresAt) {
		t.Fatalf("subscription expires_at = %v, want paid until %v (after %v)", view.ExpiresAt, sub.PaidUntil, sub.ExpiresAt)
	}
}
