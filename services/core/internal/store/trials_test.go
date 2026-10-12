package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkTgUser(t *testing.T, s *Store, tg int64) *model.User {
	t.Helper()
	u, _, err := s.RegisterTelegramUser(context.Background(), tg, model.NewToken(), model.NewUUID())
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mkTrialPlan(t *testing.T, s *Store, visibility string) *model.Plan {
	t.Helper()
	p := &model.Plan{Name: "trial-" + model.NewToken()[:6], PeriodDays: 3, Trial: true, Visibility: visibility}
	if err := s.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGrantTrialToNewcomerOnce(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	plan := mkTrialPlan(t, s, model.VisibilityPublic)
	u := mkTgUser(t, s, 5001)
	offer, err := s.TrialPlanFor(ctx, u.ID)
	if err != nil || offer == nil || offer.ID != plan.ID {
		t.Fatalf("offer before grant = %+v, %v", offer, err)
	}
	now := time.Now().UTC()
	got, err := s.GrantTrial(ctx, u.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	periods := userPeriods(t, s, u.ID)
	if len(periods) != 1 || periods[0].PlanID != plan.ID || periods[0].ID != got.PeriodID || !sameTime(periods[0].StartsAt, now) || !sameTime(periods[0].ExpiresAt, now.AddDate(0, 0, 3)) {
		t.Fatalf("periods after trial = %+v, result %+v", periods, got)
	}
	if _, err := s.GrantTrial(ctx, u.ID, now); !errors.Is(err, ErrTrialUnavailable) {
		t.Fatalf("second trial: %v", err)
	}
	if offer, err := s.TrialPlanFor(ctx, u.ID); err != nil || offer != nil {
		t.Fatalf("offer after grant = %+v, %v", offer, err)
	}
	if n := len(userPeriods(t, s, u.ID)); n != 1 {
		t.Fatalf("periods after second trial = %d", n)
	}
}

func TestTrialOnlyForUsersWithoutAnyPeriod(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	mkTrialPlan(t, s, model.VisibilityPublic)
	paid := mkLimitedPlan(t, s, 30, 1<<30)
	u := mkTgUser(t, s, 5002)
	pay(t, s, u.ID, paid.ID, time.Now().UTC().AddDate(0, -3, 0))
	if _, err := s.ExpireOverdue(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if offer, err := s.TrialPlanFor(ctx, u.ID); err != nil || offer != nil {
		t.Fatalf("offer to a former customer = %+v, %v", offer, err)
	}
	if _, err := s.GrantTrial(ctx, u.ID, time.Now().UTC()); !errors.Is(err, ErrTrialUnavailable) {
		t.Fatalf("trial for a former customer: %v", err)
	}
}

func TestTrialNeedsATrialPlanOnSale(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkTgUser(t, s, 5003)
	if _, err := s.GrantTrial(ctx, u.ID, time.Now().UTC()); !errors.Is(err, ErrTrialUnavailable) {
		t.Fatalf("trial without a plan: %v", err)
	}
	plan := mkTrialPlan(t, s, model.VisibilityPublic)
	plan.OnSale = false
	if err := s.UpdatePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if offer, err := s.TrialPlanFor(ctx, u.ID); err != nil || offer != nil {
		t.Fatalf("offer of a plan off sale = %+v, %v", offer, err)
	}
	if _, err := s.GrantTrial(ctx, u.ID, time.Now().UTC()); !errors.Is(err, ErrTrialUnavailable) {
		t.Fatalf("trial off sale: %v", err)
	}
}

func TestPrivateTrialNeedsAccess(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	plan := mkTrialPlan(t, s, model.VisibilityPrivate)
	u := mkTgUser(t, s, 5004)
	if _, err := s.GrantTrial(ctx, u.ID, time.Now().UTC()); !errors.Is(err, ErrTrialUnavailable) {
		t.Fatalf("private trial without access: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO plan_access (plan_id, user_id, source) VALUES ($1, $2, 'admin')`, plan.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if offer, err := s.TrialPlanFor(ctx, u.ID); err != nil || offer == nil {
		t.Fatalf("offer with access = %+v, %v", offer, err)
	}
	if _, err := s.GrantTrial(ctx, u.ID, time.Now().UTC()); err != nil {
		t.Fatalf("private trial with access: %v", err)
	}
}

func TestConcurrentTrialGrantsGiveOnePeriod(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	mkTrialPlan(t, s, model.VisibilityPublic)
	u := mkTgUser(t, s, 5005)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for range 8 {
		wg.Go(func() {
			if _, err := s.GrantTrial(ctx, u.ID, time.Now().UTC()); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			} else if !errors.Is(err, ErrTrialUnavailable) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if ok != 1 || len(userPeriods(t, s, u.ID)) != 1 {
		t.Fatalf("grants ok = %d, periods = %d", ok, len(userPeriods(t, s, u.ID)))
	}
}

func TestTrialDeviceOncePerHWID(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	first, second := mkTgUser(t, s, 5101), mkTgUser(t, s, 5102)
	if v, err := s.RegisterTrialDevice(ctx, first.ID, DeviceInfo{HWID: "hw-shared"}, nil); err != nil || v != DeviceAllowed {
		t.Fatalf("first claim = %v, %v", v, err)
	}
	if v, err := s.RegisterTrialDevice(ctx, second.ID, DeviceInfo{HWID: "hw-shared"}, nil); err != nil || v != DeviceTrialTaken {
		t.Fatalf("second account on the same device = %v, %v", v, err)
	}
	if n, _ := s.CountDevices(ctx, second.ID); n != 0 {
		t.Fatalf("device stored for a refused trial: %d", n)
	}
	if v, err := s.RegisterTrialDevice(ctx, first.ID, DeviceInfo{HWID: "hw-shared"}, nil); err != nil || v != DeviceAllowed {
		t.Fatalf("owner again = %v, %v", v, err)
	}
	var stored string
	if err := s.pool.QueryRow(ctx, `SELECT hwid_hash FROM trial_devices WHERE user_id = $1`, first.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "hw-shared" || len(stored) != 64 {
		t.Fatalf("stored %q, want a sha256 hex", stored)
	}
}

func TestTrialDeviceOverLimitDoesNotClaimHWID(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	full, other := mkTgUser(t, s, 5103), mkTgUser(t, s, 5104)
	limit := 1
	if v, err := s.RegisterTrialDevice(ctx, full.ID, DeviceInfo{HWID: "hw-a"}, &limit); err != nil || v != DeviceAllowed {
		t.Fatalf("first device = %v, %v", v, err)
	}
	if v, err := s.RegisterTrialDevice(ctx, full.ID, DeviceInfo{HWID: "hw-b"}, &limit); err != nil || v != DeviceOverLimit {
		t.Fatalf("over limit = %v, %v", v, err)
	}
	if v, err := s.RegisterTrialDevice(ctx, other.ID, DeviceInfo{HWID: "hw-b"}, nil); err != nil || v != DeviceAllowed {
		t.Fatalf("device refused by limit must stay free for a trial: %v, %v", v, err)
	}
}

func TestConcurrentTrialClaimsOfOneHWID(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	var users []*model.User
	for i := range 6 {
		users = append(users, mkTgUser(t, s, int64(5200+i)))
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for _, u := range users {
		wg.Go(func() {
			v, err := s.RegisterTrialDevice(ctx, u.ID, DeviceInfo{HWID: "hw-race"}, nil)
			if err != nil {
				t.Error(err)
				return
			}
			if v == DeviceAllowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if allowed != 1 {
		t.Fatalf("accounts allowed on one device = %d, want 1", allowed)
	}
}
