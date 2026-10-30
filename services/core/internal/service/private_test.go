package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func privatePlan(t *testing.T, svc *Service, name string) *model.Plan {
	t.Helper()
	p := &model.Plan{Name: name, PriceRub: 300, PeriodDays: 30, Visibility: model.VisibilityPrivate, LocationGroupID: testGroup(t, svc.store)}
	if err := svc.store.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreatePaymentNeedsAccessToAPrivatePlan(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	u, _ := userAndPlan(t, svc, 31, 100)
	vip := privatePlan(t, svc, "vip")
	var ce *ConflictError
	if _, err := svc.CreatePayment(ctx, u.ID, vip.ID); !errors.As(err, &ce) {
		t.Fatalf("private plan without access err = %v", err)
	}
	if len(fp.requests) != 0 {
		t.Fatal("provider called for a private plan without access")
	}
	if err := svc.GrantPlanAccess(ctx, vip.ID, 31); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePayment(ctx, u.ID, vip.ID); err != nil {
		t.Fatalf("private plan with access: %v", err)
	}
}

func TestRevokedAccessStillRenewsTheCurrentPrivatePlan(t *testing.T) {
	svc, _, _ := paymentsService(t)
	ctx := context.Background()
	u, _ := userAndPlan(t, svc, 32, 100)
	vip := privatePlan(t, svc, "vip")
	if err := svc.GrantPlanAccess(ctx, vip.ID, 32); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandlePayment(ctx, "manual-32", u.ID, vip.ID, 300); err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokePlanAccess(ctx, vip.ID, 32); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePayment(ctx, u.ID, vip.ID); err != nil {
		t.Fatalf("renewal after revoke refused: %v", err)
	}
	plans, err := svc.UserPlans(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		if p.ID == vip.ID {
			t.Fatal("revoked private plan still in the menu")
		}
	}
}

func TestInviteFlow(t *testing.T) {
	svc, logs := setupServiceWithLogs(t)
	ctx := context.Background()
	vip := privatePlan(t, svc, "vip")
	one := 1
	inv, err := svc.CreateInvite(ctx, vip.ID, &one, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !model.ValidInviteCode(inv.Code) {
		t.Fatalf("code %q", inv.Code)
	}
	a, _, _ := svc.RegisterUser(ctx, 41)
	b, _, _ := svc.RegisterUser(ctx, 42)
	got, already, err := svc.RedeemInvite(ctx, a.ID, inv.Code)
	if err != nil || got.ID != vip.ID || already {
		t.Fatalf("redeem = %+v, %v, %v", got, already, err)
	}
	if _, _, err := svc.RedeemInvite(ctx, b.ID, inv.Code); !errors.Is(err, store.ErrInviteUsedUp) {
		t.Fatalf("second user: %v", err)
	}
	plans, err := svc.UserPlans(ctx, a.ID)
	if err != nil || len(plans) != 1 || plans[0].ID != vip.ID {
		t.Fatalf("plans after invite = %+v, %v", plans, err)
	}
	var fe *FieldError
	if _, _, err := svc.RedeemInvite(ctx, a.ID, "bad code"); !errors.As(err, &fe) {
		t.Fatalf("bad format: %v", err)
	}
	if _, _, err := svc.RedeemInvite(ctx, 424242, inv.Code); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	if strings.Contains(logs.String(), inv.Code) {
		t.Fatal("invite code leaked into logs")
	}
	if !strings.Contains(logs.String(), "invite_redeemed") || !strings.Contains(logs.String(), "invite_rejected") {
		t.Fatalf("logs: %s", logs.String())
	}
}

func TestCreateInviteValidation(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	vip := privatePlan(t, svc, "vip")
	zero, past := 0, time.Now().Add(-time.Hour)
	var fe *FieldError
	if _, err := svc.CreateInvite(ctx, vip.ID, &zero, nil); !errors.As(err, &fe) || fe.Field != "max_uses" {
		t.Fatalf("zero uses: %v", err)
	}
	if _, err := svc.CreateInvite(ctx, vip.ID, nil, &past); !errors.As(err, &fe) || fe.Field != "expires_at" {
		t.Fatalf("past expiry: %v", err)
	}
	if _, err := svc.CreateInvite(ctx, 424242, nil, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown plan: %v", err)
	}
}

func TestGrantAccessRegistersUnknownTelegramUser(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	vip := privatePlan(t, svc, "vip")
	if err := svc.GrantPlanAccess(ctx, vip.ID, 77001); err != nil {
		t.Fatal(err)
	}
	u, err := svc.GetUserByTelegramID(ctx, 77001)
	if err != nil {
		t.Fatal(err)
	}
	plans, _ := svc.UserPlans(ctx, u.ID)
	if len(plans) != 1 {
		t.Fatalf("plans = %+v", plans)
	}
	list, err := svc.ListPlanAccess(ctx, vip.ID)
	if err != nil || len(list) != 1 || *list[0].TelegramID != 77001 {
		t.Fatalf("access = %+v, %v", list, err)
	}
	if err := svc.GrantPlanAccess(ctx, 424242, 77001); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown plan: %v", err)
	}
	if err := svc.RevokePlanAccess(ctx, vip.ID, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoke for unknown user: %v", err)
	}
}
