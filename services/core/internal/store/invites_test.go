package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkVisPlan(t *testing.T, s *Store, name string, price int64, visibility string) *model.Plan {
	t.Helper()
	p := &model.Plan{Name: name, PriceRub: price, PeriodDays: 30, Visibility: visibility}
	if err := s.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func mkInvite(t *testing.T, s *Store, planID int64, maxUses *int, expires *time.Time) *model.Invite {
	t.Helper()
	inv := &model.Invite{Code: model.NewInviteCode(), PlanID: planID, MaxUses: maxUses, ExpiresAt: expires}
	if err := s.CreateInvite(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	return inv
}

func planIDs(plans []model.Plan) []int64 {
	out := []int64{}
	for _, p := range plans {
		out = append(out, p.ID)
	}
	return out
}

func TestVisiblePlansRespectAccess(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	public := mkVisPlan(t, s, "public", 200, model.VisibilityPublic)
	private := mkVisPlan(t, s, "private", 100, model.VisibilityPrivate)
	mkVisPlan(t, s, "free", 0, model.VisibilityPublic)
	hidden := mkVisPlan(t, s, "hidden", 50, model.VisibilityPrivate)
	hidden.OnSale = false
	if err := s.UpdatePlan(ctx, hidden); err != nil {
		t.Fatal(err)
	}
	u, other := mkTgUser(t, s, 6001), mkTgUser(t, s, 6002)
	got, err := s.VisiblePlans(ctx, u.ID)
	if err != nil || len(got) != 1 || got[0].ID != public.ID {
		t.Fatalf("without access = %v, %v", planIDs(got), err)
	}
	for _, p := range []*model.Plan{private, hidden} {
		if err := s.GrantPlanAccess(ctx, p.ID, u.ID, AccessAdmin, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.VisiblePlans(ctx, u.ID)
	if err != nil || len(got) != 2 || got[0].ID != private.ID || got[1].ID != public.ID {
		t.Fatalf("with access = %v, %v", planIDs(got), err)
	}
	if ok, err := s.HasPlanAccess(ctx, private.ID, u.ID); err != nil || !ok {
		t.Fatalf("has access = %v, %v", ok, err)
	}
	if ok, err := s.HasPlanAccess(ctx, private.ID, other.ID); err != nil || ok {
		t.Fatalf("other has access = %v, %v", ok, err)
	}
	if got, _ := s.VisiblePlans(ctx, other.ID); len(got) != 1 {
		t.Fatalf("other user sees %v", planIDs(got))
	}
	if err := s.GrantPlanAccess(ctx, private.ID, u.ID, AccessAdmin, nil); err != nil {
		t.Fatalf("repeated grant: %v", err)
	}
	list, err := s.ListPlanAccess(ctx, private.ID)
	if err != nil || len(list) != 1 || list[0].UserID != u.ID || list[0].TelegramID == nil || *list[0].TelegramID != 6001 || list[0].Source != AccessAdmin {
		t.Fatalf("access list = %+v, %v", list, err)
	}
	if err := s.RevokePlanAccess(ctx, private.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokePlanAccess(ctx, private.ID, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if got, _ := s.VisiblePlans(ctx, u.ID); len(got) != 1 {
		t.Fatalf("after revoke sees %v", planIDs(got))
	}
}

func TestRedeemInviteRules(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	plan := mkVisPlan(t, s, "friends", 100, model.VisibilityPrivate)
	two := 2
	inv := mkInvite(t, s, plan.ID, &two, nil)
	a, b, c := mkTgUser(t, s, 6101), mkTgUser(t, s, 6102), mkTgUser(t, s, 6103)
	now := time.Now().UTC()
	if r, err := s.RedeemInvite(ctx, inv.Code, a.ID, now); err != nil || r.PlanID != plan.ID || r.Already {
		t.Fatalf("first = %+v, %v", r, err)
	}
	if r, err := s.RedeemInvite(ctx, inv.Code, a.ID, now); err != nil || !r.Already {
		t.Fatalf("repeat = %+v, %v", r, err)
	}
	if _, err := s.RedeemInvite(ctx, inv.Code, b.ID, now); err != nil {
		t.Fatalf("second user: %v", err)
	}
	if _, err := s.RedeemInvite(ctx, inv.Code, c.ID, now); !errors.Is(err, ErrInviteUsedUp) {
		t.Fatalf("third user: %v", err)
	}
	list, err := s.ListInvites(ctx, plan.ID)
	if err != nil || len(list) != 1 || list[0].Uses != 2 {
		t.Fatalf("invites = %+v, %v", list, err)
	}
	access, _ := s.ListPlanAccess(ctx, plan.ID)
	if len(access) != 2 || access[0].Source != AccessInvite || access[0].InviteID == nil || *access[0].InviteID != inv.ID {
		t.Fatalf("access = %+v", access)
	}
	past := now.Add(-time.Minute)
	expired := mkInvite(t, s, plan.ID, nil, &past)
	if _, err := s.RedeemInvite(ctx, expired.Code, c.ID, now); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expired: %v", err)
	}
	open := mkInvite(t, s, plan.ID, nil, nil)
	if _, err := s.RevokeInvite(ctx, open.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemInvite(ctx, open.Code, c.ID, now); !errors.Is(err, ErrInviteRevoked) {
		t.Fatalf("revoked: %v", err)
	}
	if _, err := s.RedeemInvite(ctx, "AAAAAAAAAAAAAAAAAAAAAA", c.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := s.RevokeInvite(ctx, 424242, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown: %v", err)
	}
	if ok, _ := s.HasPlanAccess(ctx, plan.ID, c.ID); ok {
		t.Fatal("refused invites granted access")
	}
}

func TestConcurrentRedeemsRespectMaxUses(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	plan := mkVisPlan(t, s, "friends", 100, model.VisibilityPrivate)
	one := 1
	inv := mkInvite(t, s, plan.ID, &one, nil)
	var users []*model.User
	for i := range 6 {
		users = append(users, mkTgUser(t, s, int64(6200+i)))
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for _, u := range users {
		wg.Go(func() {
			_, err := s.RedeemInvite(ctx, inv.Code, u.ID, time.Now().UTC())
			switch {
			case err == nil:
				mu.Lock()
				ok++
				mu.Unlock()
			case !errors.Is(err, ErrInviteUsedUp):
				t.Error(err)
			}
		})
	}
	wg.Wait()
	access, _ := s.ListPlanAccess(ctx, plan.ID)
	if ok != 1 || len(access) != 1 {
		t.Fatalf("redeemed = %d, access = %d", ok, len(access))
	}
}
