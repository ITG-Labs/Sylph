package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func trialFixture(t *testing.T, hwid bool) (*Service, *model.Plan) {
	t.Helper()
	svc := setupService(t)
	p := &model.Plan{Name: "trial", PeriodDays: 3, Trial: true, HWIDRequired: hwid, Visibility: model.VisibilityPublic}
	if err := svc.store.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return svc, p
}

func trialUser(t *testing.T, svc *Service, tg int64) *model.User {
	t.Helper()
	u, _, err := svc.RegisterUser(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GrantTrial(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	return u
}

func viewOf(t *testing.T, svc *Service, u *model.User, hwid string) *SubscriptionView {
	t.Helper()
	v, err := svc.Subscription(context.Background(), SubscriptionRequest{SubToken: u.SubToken, HWID: hwid, UserAgent: "Happ/1.6.0"})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTrialOnePerDeviceAcrossAccounts(t *testing.T) {
	svc, _ := trialFixture(t, true)
	first, second := trialUser(t, svc, 801), trialUser(t, svc, 802)
	if v := viewOf(t, svc, first, ""); v.Status != StatusHWIDRequired {
		t.Fatalf("trial without hwid = %s", v.Status)
	}
	if v := viewOf(t, svc, first, "phone-1"); v.Status != StatusOK {
		t.Fatalf("first account = %s", v.Status)
	}
	if v := viewOf(t, svc, second, "phone-1"); v.Status != StatusTrialUsed || len(v.Connections) != 0 {
		t.Fatalf("second account on the same phone = %+v", v)
	}
	if v := viewOf(t, svc, second, "phone-2"); v.Status != StatusOK {
		t.Fatalf("second account on its own phone = %s", v.Status)
	}
}

func TestTrialWithoutHWIDFlagIgnoresDevices(t *testing.T) {
	svc, _ := trialFixture(t, false)
	first, second := trialUser(t, svc, 811), trialUser(t, svc, 812)
	for _, u := range []*model.User{first, second} {
		if v := viewOf(t, svc, u, "phone-1"); v.Status != StatusOK {
			t.Fatalf("user %d = %s", u.ID, v.Status)
		}
		if v := viewOf(t, svc, u, ""); v.Status != StatusOK {
			t.Fatalf("user %d without hwid = %s", u.ID, v.Status)
		}
	}
}

func TestPaidPlanIgnoresTrialDevices(t *testing.T) {
	svc, _ := trialFixture(t, true)
	first := trialUser(t, svc, 821)
	if v := viewOf(t, svc, first, "phone-1"); v.Status != StatusOK {
		t.Fatalf("trial = %s", v.Status)
	}
	paid := &model.Plan{Name: "month", PriceRub: 100, PeriodDays: 30, HWIDRequired: true, Visibility: model.VisibilityPublic}
	if err := svc.store.CreatePlan(context.Background(), paid); err != nil {
		t.Fatal(err)
	}
	buyer, _, err := svc.RegisterUser(context.Background(), 822)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.ApplyPayment(context.Background(), buyer.ID, paid.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if v := viewOf(t, svc, buyer, "phone-1"); v.Status != StatusOK {
		t.Fatalf("paid plan on a trial phone = %s", v.Status)
	}
}

func TestGrantTrialUnavailable(t *testing.T) {
	svc, _ := trialFixture(t, true)
	u := trialUser(t, svc, 831)
	if _, err := svc.GrantTrial(context.Background(), u.ID); !errors.Is(err, store.ErrTrialUnavailable) {
		t.Fatalf("second trial: %v", err)
	}
	if _, err := svc.GrantTrial(context.Background(), 424242); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}
