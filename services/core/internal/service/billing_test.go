package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

var testPools sync.Map

func setupService(t *testing.T) *Service {
	t.Helper()
	pool := store.TestPoolWithSchema(t, "service_test")
	ctx := context.Background()
	if err := store.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(pool)
	svc := NewService(st, slog.New(slog.DiscardHandler))
	testPools.Store(svc, pool)
	return svc
}

func poolOf(svc *Service) *pgxpool.Pool {
	p, _ := testPools.Load(svc)
	return p.(*pgxpool.Pool)
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
	p := &model.Plan{Name: "m", PriceRub: 150, PeriodDays: 30, LocationGroupID: testGroup(t, svc.store)}
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
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30, LocationGroupID: testGroup(t, svc.store)}
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

func TestRegisterUserTwiceReturnsTheSameUser(t *testing.T) {
	svc, logs := setupServiceWithLogs(t)
	ctx := context.Background()
	first, _, err := svc.RegisterUser(ctx, 300)
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := svc.RegisterUser(ctx, 300)
	if err != nil || again.ID != first.ID || again.SubToken != first.SubToken {
		t.Fatalf("second register = %+v, %v", again, err)
	}
	if n := strings.Count(logs.String(), `"msg":"user_registered"`); n != 1 {
		t.Fatalf("user_registered logged %d times", n)
	}
}

func TestUserStatusWithoutSubscription(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, _, err := svc.RegisterUser(ctx, 310)
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.GetUserStatus(ctx, 310)
	if err != nil {
		t.Fatal(err)
	}
	if st.User.ID != u.ID || st.Subscription != nil || st.Plan != nil || st.Devices != 0 {
		t.Fatalf("status = %+v", st)
	}
	if _, err := svc.GetUserStatus(ctx, 311); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user err = %v", err)
	}
}

func TestUserStatusWithSubscriptionAndDevices(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, _, _ := svc.RegisterUser(ctx, 320)
	limit := 2
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30, DeviceLimit: &limit, HWIDRequired: true, LocationGroupID: testGroup(t, svc.store)}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandlePayment(ctx, "pay-320", u.ID, p.ID, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RegisterDevice(ctx, u.ID, store.DeviceInfo{HWID: "hwid-320-a"}, &limit); err != nil {
		t.Fatal(err)
	}
	st, err := svc.GetUserStatus(ctx, 320)
	if err != nil {
		t.Fatal(err)
	}
	if st.Subscription == nil || st.Subscription.PaidUntil == nil || st.Plan == nil || st.Plan.ID != p.ID || st.Devices != 1 {
		t.Fatalf("status = %+v", st)
	}
}

func TestRotateUserGivesANewLink(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, key, _ := svc.RegisterUser(ctx, 330)
	token, err := svc.RotateUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || token == u.SubToken {
		t.Fatalf("token = %q", token)
	}
	keys, _ := svc.store.GetUserKeys(ctx, u.ID)
	if len(keys) != 1 || keys[0].UUID == key {
		t.Fatalf("keys after rotate = %+v", keys)
	}
	if _, err := svc.RotateUser(ctx, 424242); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user err = %v", err)
	}
}
