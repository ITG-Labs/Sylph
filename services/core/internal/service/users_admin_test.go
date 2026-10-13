package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func TestBannedUserIsInvisibleToServiceCalls(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	svc, logs := f.svc, &logBuffer{}
	svc.log = slog.New(slog.NewJSONHandler(logs, nil))
	ctx := context.Background()
	if already, err := svc.BanUser(ctx, f.user.ID, "spam"); err != nil || already {
		t.Fatalf("ban: already %v, %v", already, err)
	}
	if _, err := svc.Subscription(ctx, SubscriptionRequest{SubToken: f.user.SubToken}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("subscription of a banned user: %v", err)
	}
	if !logs.has("subscription_view") || !strings.Contains(logs.String(), `"status":"banned"`) {
		t.Fatal("subscription of a banned user is not logged")
	}
	if _, err := svc.GetUserStatus(ctx, *f.user.TelegramID); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("status: %v", err)
	}
	if _, err := svc.UserPlans(ctx, f.user.ID); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("plans: %v", err)
	}
	if _, err := svc.UserStats(ctx, f.user.ID); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("stats: %v", err)
	}
	if _, err := svc.ListUserDevices(ctx, f.user.ID); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("devices: %v", err)
	}
	if err := svc.ResetUserDevices(ctx, f.user.ID); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("devices reset: %v", err)
	}
	if err := svc.RemoveUserDevice(ctx, f.user.ID, 1); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("device remove: %v", err)
	}
	if _, err := svc.RotateUser(ctx, f.user.ID); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := svc.GrantTrial(ctx, f.user.ID); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("trial: %v", err)
	}
	if _, _, err := svc.RedeemInvite(ctx, f.user.ID, model.NewInviteCode()); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("invite: %v", err)
	}
	u, _, err := svc.RegisterUser(ctx, *f.user.TelegramID)
	if err != nil || u.Status != model.UserBanned {
		t.Fatalf("register returns the banned user: %+v, %v", u, err)
	}
}

func TestBannedUserCannotStartAPayment(t *testing.T) {
	svc, _, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 501, 199)
	if _, err := svc.BanUser(ctx, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePayment(ctx, u.ID, p.ID); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("create payment: %v", err)
	}
}

func TestPaymentPaidAfterTheBanIsAppliedButNotReported(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, p := userAndPlan(t, svc, 502, 199)
	pay, err := svc.CreatePayment(ctx, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BanUser(ctx, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	succeed(fp, pay.PaymentID)
	if _, err := svc.CheckPayment(ctx, pay.PaymentID); !errors.Is(err, store.ErrUserBanned) {
		t.Fatalf("check of a banned user's payment: %v", err)
	}
	got, err := svc.store.GetPayment(ctx, pay.PaymentID)
	if err != nil || got.Status != "succeeded" {
		t.Fatalf("payment after check: %+v, %v", got, err)
	}
}

func TestAdminCannotManageABannedUser(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	ctx := context.Background()
	if _, err := f.svc.BanUser(ctx, f.user.ID, ""); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := f.svc.ExtendSubscription(ctx, f.user.ID, 5); !errors.As(err, &ce) || ce.Field != "user_id" {
		t.Fatalf("extend: %v", err)
	}
	if _, err := f.svc.RecordPayment(ctx, "manual-1", f.user.ID, f.plan.ID, 0); !errors.As(err, &ce) || ce.Field != "user_id" {
		t.Fatalf("manual payment: %v", err)
	}
	vip := &model.Plan{Name: "vip", PriceRub: 99, PeriodDays: 30, Visibility: model.VisibilityPrivate}
	if err := f.svc.CreatePlanChecked(ctx, vip); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.GrantPlanAccess(ctx, vip.ID, *f.user.TelegramID); !errors.As(err, &ce) || ce.Field != "telegram_id" {
		t.Fatalf("grant access: %v", err)
	}
	if already, err := f.svc.UnbanUser(ctx, f.user.ID); err != nil || already {
		t.Fatalf("unban: already %v, %v", already, err)
	}
	if _, err := f.svc.ExtendSubscription(ctx, f.user.ID, 5); !errors.As(err, &ce) || ce.Field != "subscription" {
		t.Fatalf("extend after unban: %v", err)
	}
}

func TestManualSubscriptionInputIsChecked(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	ctx := context.Background()
	var fe *FieldError
	for _, days := range []int{0, -1, 3651} {
		if _, err := f.svc.ExtendSubscription(ctx, f.user.ID, days); !errors.As(err, &fe) || fe.Field != "days" {
			t.Fatalf("days %d: %v", days, err)
		}
	}
	for _, b := range []int64{0, -1, 1<<50 + 1} {
		if _, err := f.svc.AddTraffic(ctx, f.user.ID, b); !errors.As(err, &fe) || fe.Field != "bytes" {
			t.Fatalf("bytes %d: %v", b, err)
		}
	}
	if _, err := f.svc.BanUser(ctx, f.user.ID, "bad\x00reason"); !errors.As(err, &fe) || fe.Field != "reason" {
		t.Fatalf("reason with a control character: %v", err)
	}
	var ce *ConflictError
	if _, err := f.svc.AddTraffic(ctx, f.user.ID, 10); !errors.As(err, &ce) || ce.Field != "bytes" {
		t.Fatalf("bonus on an unlimited plan: %v", err)
	}
	if _, err := f.svc.ExtendSubscription(ctx, 999999, 5); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestSubscriptionCountsTheTrafficBonus(t *testing.T) {
	limit := int64(1000)
	f := newSubFixture(t, nil, &limit)
	ctx := context.Background()
	f.spend(t, 1000)
	if v := f.view(t, ""); v.Status != StatusTrafficExceeded {
		t.Fatalf("status before the bonus = %s", v.Status)
	}
	sub, err := f.svc.AddTraffic(ctx, f.user.ID, 500)
	if err != nil {
		t.Fatal(err)
	}
	if sub.TrafficLimitBytes == nil || *sub.TrafficLimitBytes != 1500 {
		t.Fatalf("limit after the bonus = %v", sub.TrafficLimitBytes)
	}
	v := f.view(t, "")
	if v.Status != StatusOK || v.TrafficLimitBytes == nil || *v.TrafficLimitBytes != 1500 {
		t.Fatalf("view after the bonus: %s, limit %v", v.Status, v.TrafficLimitBytes)
	}
	st, err := f.svc.GetUserStatus(ctx, *f.user.TelegramID)
	if err != nil || st.Subscription.TrafficLimitBytes == nil || *st.Subscription.TrafficLimitBytes != 1500 {
		t.Fatalf("status for the bot: %+v, %v", st, err)
	}
}
