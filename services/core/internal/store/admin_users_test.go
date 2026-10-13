package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestTrafficBonusBringsExhaustedUserBack(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, k := userWithKey(t, s)
	p := mkLimitedPlan(t, s, 30, 1000)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	n := deltaNode(t, s)
	record(t, s, n.ID, "b-1", model.TrafficDelta{KeyUUID: k, BytesUp: 1000})
	if keys, _ := s.ListNodeUserKeys(ctx, n.ID); len(keys) != 0 {
		t.Fatal("exhausted user still on the node")
	}
	v := nodeVersion(t, s, n.CertCN)
	sub, err := s.AddTrafficBonus(ctx, u.ID, 500, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if sub.TrafficBonusBytes != 500 || sub.TrafficLimitBytes == nil || *sub.TrafficLimitBytes != 1500 {
		t.Fatalf("bonus %d, limit %v", sub.TrafficBonusBytes, sub.TrafficLimitBytes)
	}
	if nodeVersion(t, s, n.CertCN) <= v {
		t.Fatal("bonus did not bump the nodes")
	}
	if keys, _ := s.ListNodeUserKeys(ctx, n.ID); len(keys) != 1 {
		t.Fatal("user did not come back after the bonus")
	}
	active, err := s.GetActiveSubscription(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.TrafficBonusBytes != 500 || active.TrafficLimitBytes == nil || *active.TrafficLimitBytes != 1500 {
		t.Fatalf("active period: bonus %d, limit %v", active.TrafficBonusBytes, active.TrafficLimitBytes)
	}
	res := record(t, s, n.ID, "b-2", model.TrafficDelta{KeyUUID: k, BytesUp: 500})
	if len(res.Exhausted) != 1 || res.Exhausted[0].Limit != 1500 {
		t.Fatalf("exhausted after the bonus = %+v", res.Exhausted)
	}
	if keys, _ := s.ListNodeUserKeys(ctx, n.ID); len(keys) != 0 {
		t.Fatal("user exhausted the bonus but stays on the node")
	}
}

func TestTrafficBonusSaturates(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkLimitedPlan(t, s, 30, 1<<62)
	pay(t, s, u.ID, p.ID, time.Now().UTC())
	for range 3 {
		if _, err := s.AddTrafficBonus(ctx, u.ID, 1<<62, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	sub, err := s.GetActiveSubscription(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub.TrafficLimitBytes == nil || *sub.TrafficLimitBytes <= 1<<62 {
		t.Fatalf("limit = %v", sub.TrafficLimitBytes)
	}
}

func TestTrafficBonusNeedsALimitedActivePeriod(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	if _, err := s.AddTrafficBonus(ctx, u.ID, 1, time.Now().UTC()); !errors.Is(err, ErrNoActivePeriod) {
		t.Fatalf("without a period: %v", err)
	}
	pay(t, s, u.ID, mkPlan(t, s).ID, time.Now().UTC())
	if _, err := s.AddTrafficBonus(ctx, u.ID, 1, time.Now().UTC()); !errors.Is(err, ErrUnlimitedPlan) {
		t.Fatalf("unlimited plan: %v", err)
	}
	if _, err := s.AddTrafficBonus(ctx, 999999, 1, time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestBonusKeepsSamePlanPaymentQueued(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkLimitedPlan(t, s, 30, 1000)
	now := time.Now().UTC()
	first := pay(t, s, u.ID, p.ID, now)
	setUsed(t, s, first.PeriodID, 1000)
	if _, err := s.AddTrafficBonus(ctx, u.ID, 10, now); err != nil {
		t.Fatal(err)
	}
	if res := pay(t, s, u.ID, p.ID, now); res.StartsNow {
		t.Fatal("period with bonus left counted as exhausted: payment started now")
	}
}

func TestExtendShiftsTheQueue(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	now := time.Now().UTC()
	pay(t, s, u.ID, p.ID, now)
	pay(t, s, u.ID, p.ID, now)
	before := userPeriods(t, s, u.ID)
	sub, err := s.ExtendPeriod(ctx, u.ID, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	after := userPeriods(t, s, u.ID)
	shift := 5 * day
	if !sameTime(after[0].ExpiresAt, before[0].ExpiresAt.Add(shift)) || !sameTime(after[0].StartsAt, before[0].StartsAt) {
		t.Fatalf("current: %v-%v -> %v-%v", before[0].StartsAt, before[0].ExpiresAt, after[0].StartsAt, after[0].ExpiresAt)
	}
	if !sameTime(after[1].StartsAt, before[1].StartsAt.Add(shift)) || !sameTime(after[1].ExpiresAt, before[1].ExpiresAt.Add(shift)) {
		t.Fatalf("queued: %v-%v -> %v-%v", before[1].StartsAt, before[1].ExpiresAt, after[1].StartsAt, after[1].ExpiresAt)
	}
	if sub.PaidUntil == nil || !sameTime(*sub.PaidUntil, before[1].ExpiresAt.Add(shift)) {
		t.Fatalf("paid until = %v", sub.PaidUntil)
	}
	if _, err := s.ExtendPeriod(ctx, mkUser(t, s).ID, 5, now); !errors.Is(err, ErrNoActivePeriod) {
		t.Fatalf("without a period: %v", err)
	}
}

func TestChangePeriodPlanKeepsDatesAndCounter(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	other := mkLimitedPlan(t, s, 7, 5000)
	now := time.Now().UTC()
	first := pay(t, s, u.ID, p.ID, now)
	pay(t, s, u.ID, p.ID, now)
	setUsed(t, s, first.PeriodID, 42)
	before := userPeriods(t, s, u.ID)
	n := deltaNode(t, s)
	v := nodeVersion(t, s, n.CertCN)
	if changed, err := s.ChangePeriodPlan(ctx, u.ID, p.ID, now); err != nil || changed {
		t.Fatalf("same plan: changed %v, %v", changed, err)
	}
	if nodeVersion(t, s, n.CertCN) != v {
		t.Fatal("same plan bumped the nodes")
	}
	if changed, err := s.ChangePeriodPlan(ctx, u.ID, other.ID, now); err != nil || !changed {
		t.Fatalf("other plan: changed %v, %v", changed, err)
	}
	after := userPeriods(t, s, u.ID)
	if after[0].PlanID != other.ID || after[0].TrafficUsedBytes != 42 || !sameTime(after[0].ExpiresAt, before[0].ExpiresAt) {
		t.Fatalf("current after change: %+v", after[0])
	}
	if after[1].PlanID != p.ID {
		t.Fatal("queued period changed its plan")
	}
	if _, err := s.ChangePeriodPlan(ctx, u.ID, 999999, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown plan: %v", err)
	}
}

func TestCloseCurrentPeriodStartsTheNextNow(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	now := time.Now().UTC()
	first := pay(t, s, u.ID, p.ID, now.Add(-10*day))
	pay(t, s, u.ID, p.ID, now)
	pay(t, s, u.ID, p.ID, now)
	before := userPeriods(t, s, u.ID)
	if err := s.ClosePeriod(ctx, first.PeriodID, now); err != nil {
		t.Fatal(err)
	}
	after := userPeriods(t, s, u.ID)
	if !after[0].Canceled || !sameTime(after[0].ExpiresAt, now) {
		t.Fatalf("closed period: %+v", after[0])
	}
	remainder := before[0].ExpiresAt.Sub(now)
	for i := 1; i < 3; i++ {
		if !sameTime(after[i].StartsAt, before[i].StartsAt.Add(-remainder)) || !sameTime(after[i].ExpiresAt, before[i].ExpiresAt.Add(-remainder)) {
			t.Fatalf("period %d: %v-%v -> %v-%v", i, before[i].StartsAt, before[i].ExpiresAt, after[i].StartsAt, after[i].ExpiresAt)
		}
	}
	active, err := s.GetActiveSubscription(ctx, u.ID)
	if err != nil || active.ID != after[1].ID {
		t.Fatalf("active after close = %+v, %v", active, err)
	}
	if err := s.ClosePeriod(ctx, first.PeriodID, now); !errors.Is(err, ErrPeriodClosed) {
		t.Fatalf("closing twice: %v", err)
	}
	if err := s.ClosePeriod(ctx, 999999, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown period: %v", err)
	}
}

func TestCloseQueuedPeriodShiftsTheLaterOnes(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	now := time.Now().UTC()
	pay(t, s, u.ID, p.ID, now)
	second := pay(t, s, u.ID, p.ID, now)
	pay(t, s, u.ID, p.ID, now)
	before := userPeriods(t, s, u.ID)
	if err := s.ClosePeriod(ctx, second.PeriodID, now); err != nil {
		t.Fatal(err)
	}
	after := userPeriods(t, s, u.ID)
	if !after[1].Canceled {
		t.Fatal("queued period not closed")
	}
	if !sameTime(after[0].ExpiresAt, before[0].ExpiresAt) || after[0].Canceled {
		t.Fatal("current period changed")
	}
	if !sameTime(after[2].StartsAt, before[0].ExpiresAt) || !sameTime(after[2].ExpiresAt, before[1].ExpiresAt) {
		t.Fatalf("last period: %v-%v, want %v-%v", after[2].StartsAt, after[2].ExpiresAt, before[0].ExpiresAt, before[1].ExpiresAt)
	}
}

func TestBanClosesEverythingAndLeavesTheNodes(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, _ := userWithKey(t, s)
	p := mkPlan(t, s)
	vip := mkVisPlan(t, s, "vip", 99, model.VisibilityPrivate)
	now := time.Now().UTC()
	pay(t, s, u.ID, p.ID, now)
	pay(t, s, u.ID, p.ID, now)
	if err := s.GrantPlanAccess(ctx, vip.ID, u.ID, "admin", nil); err != nil {
		t.Fatal(err)
	}
	n := deltaNode(t, s)
	if keys, _ := s.ListNodeUserKeys(ctx, n.ID); len(keys) != 1 {
		t.Fatal("paid user not on the node")
	}
	v := nodeVersion(t, s, n.CertCN)
	already, err := s.BanUser(ctx, u.ID, "spam", now)
	if err != nil || already {
		t.Fatalf("ban: already %v, %v", already, err)
	}
	if nodeVersion(t, s, n.CertCN) <= v {
		t.Fatal("ban did not bump the nodes")
	}
	if keys, _ := s.ListNodeUserKeys(ctx, n.ID); len(keys) != 0 {
		t.Fatal("banned user still on the node")
	}
	for i, ps := range userPeriods(t, s, u.ID) {
		if !ps.Canceled {
			t.Fatalf("period %d left open", i)
		}
	}
	if _, err := s.GetActiveSubscription(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("active period after ban: %v", err)
	}
	if ok, _ := s.HasPlanAccess(ctx, vip.ID, u.ID); ok {
		t.Fatal("private access survived the ban")
	}
	got, err := s.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.UserBanned || got.BannedAt == nil || got.BanReason == nil || *got.BanReason != "spam" {
		t.Fatalf("user after ban: %+v", got)
	}
	v = nodeVersion(t, s, n.CertCN)
	if already, err := s.BanUser(ctx, u.ID, "again", now); err != nil || !already {
		t.Fatalf("second ban: already %v, %v", already, err)
	}
	if nodeVersion(t, s, n.CertCN) != v {
		t.Fatal("repeated ban bumped the nodes")
	}
	if got, _ := s.GetUserByID(ctx, u.ID); *got.BanReason != "spam" {
		t.Fatal("repeated ban overwrote the reason")
	}
}

func TestUnbanStartsFromScratch(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	pay(t, s, u.ID, mkPlan(t, s).ID, time.Now().UTC())
	if already, err := s.UnbanUser(ctx, u.ID); err != nil || !already {
		t.Fatalf("unban of an active user: already %v, %v", already, err)
	}
	if _, err := s.BanUser(ctx, u.ID, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if already, err := s.UnbanUser(ctx, u.ID); err != nil || already {
		t.Fatalf("unban: already %v, %v", already, err)
	}
	got, _ := s.GetUserByID(ctx, u.ID)
	if got.Status != model.UserActive || got.BannedAt != nil || got.BanReason != nil {
		t.Fatalf("user after unban: %+v", got)
	}
	if _, err := s.GetActiveSubscription(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("unban brought a period back")
	}
	if _, err := s.BanUser(ctx, 999999, "", time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ban of unknown user: %v", err)
	}
}

func TestBannedUserCannotBeManaged(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkLimitedPlan(t, s, 30, 1000)
	now := time.Now().UTC()
	if _, err := s.BanUser(ctx, u.ID, "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTrafficBonus(ctx, u.ID, 1, now); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("bonus: %v", err)
	}
	if _, err := s.ExtendPeriod(ctx, u.ID, 1, now); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("extend: %v", err)
	}
	if _, err := s.ChangePeriodPlan(ctx, u.ID, p.ID, now); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("change plan: %v", err)
	}
	if _, err := s.RecordManualPayment(ctx, &model.Payment{PaymentID: "m-1", UserID: u.ID, PlanID: p.ID, AmountRub: 1}, now); !errors.Is(err, ErrUserBanned) {
		t.Fatalf("manual payment: %v", err)
	}
	if _, err := s.GetPayment(ctx, "m-1"); !errors.Is(err, ErrNotFound) {
		t.Fatal("refused manual payment left a row")
	}
}
