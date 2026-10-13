package store

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func tgUser(t *testing.T, s *Store, tg int64) *model.User {
	t.Helper()
	u, _, err := s.RegisterTelegramUser(context.Background(), tg, model.NewToken(), model.NewUUID())
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func userIDs(rows []UserRow) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func listUsers(t *testing.T, s *Store, f UserFilter) []UserRow {
	t.Helper()
	rows, err := s.ListUsers(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestListUsersSearchAndFilters(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	a := tgUser(t, s, 1001)
	b := tgUser(t, s, 1002)
	c := tgUser(t, s, 1003)
	p := mkLimitedPlan(t, s, 30, 5000)
	res := pay(t, s, a.ID, p.ID, time.Now().UTC())
	setUsed(t, s, res.PeriodID, 70)
	if _, err := s.BanUser(ctx, c.ID, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got := userIDs(listUsers(t, s, UserFilter{Limit: 50})); len(got) != 3 || got[0] != c.ID || got[2] != a.ID {
		t.Fatalf("all users = %v", got)
	}
	if got := userIDs(listUsers(t, s, UserFilter{Number: &[]int64{1002}[0], Limit: 50})); len(got) != 1 || got[0] != b.ID {
		t.Fatalf("by telegram id = %v", got)
	}
	if got := userIDs(listUsers(t, s, UserFilter{Number: &a.ID, Limit: 50})); len(got) != 1 || got[0] != a.ID {
		t.Fatalf("by id = %v", got)
	}
	if got := userIDs(listUsers(t, s, UserFilter{Token: b.SubToken, Limit: 50})); len(got) != 1 || got[0] != b.ID {
		t.Fatalf("by token = %v", got)
	}
	if got := userIDs(listUsers(t, s, UserFilter{Status: model.UserBanned, Limit: 50})); len(got) != 1 || got[0] != c.ID {
		t.Fatalf("banned = %v", got)
	}
	if got := userIDs(listUsers(t, s, UserFilter{Subscription: "active", Limit: 50})); len(got) != 1 || got[0] != a.ID {
		t.Fatalf("with a subscription = %v", got)
	}
	if got := userIDs(listUsers(t, s, UserFilter{Subscription: "none", Limit: 50})); len(got) != 2 {
		t.Fatalf("without a subscription = %v", got)
	}
	if got := userIDs(listUsers(t, s, UserFilter{PlanID: &p.ID, Limit: 50})); len(got) != 1 || got[0] != a.ID {
		t.Fatalf("by plan = %v", got)
	}
	page := listUsers(t, s, UserFilter{Limit: 2})
	if len(page) != 2 {
		t.Fatalf("first page = %v", userIDs(page))
	}
	rest := listUsers(t, s, UserFilter{Limit: 2, Before: page[1].ID})
	if got := userIDs(rest); len(got) != 1 || got[0] != a.ID {
		t.Fatalf("second page = %v", got)
	}
	row := rest[0]
	if row.Plan == nil || row.Plan.ID != p.ID || row.TrafficUsedBytes != 70 || row.TrafficLimitBytes == nil || *row.TrafficLimitBytes != 5000 || row.PaidUntil == nil {
		t.Fatalf("row = %+v", row)
	}
	if row.TelegramID == nil || *row.TelegramID != 1001 || row.Status != model.UserActive {
		t.Fatalf("row user fields = %+v", row)
	}
}

func TestSettleLinksThePaymentToItsPeriod(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	pp := mkProviderPayment(t, s, u.ID, p.ID)
	res, err := s.SettlePayment(ctx, pp.PaymentID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	got := paymentRow(t, s, pp.PaymentID)
	if got.PeriodID == nil || *got.PeriodID != res.Period.PeriodID {
		t.Fatalf("period of the payment = %v, want %d", got.PeriodID, res.Period.PeriodID)
	}
	manual, err := s.RecordManualPayment(ctx, &model.Payment{PaymentID: "m-1", UserID: u.ID, PlanID: p.ID, AmountRub: 5}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got := paymentRow(t, s, "m-1"); got.PeriodID == nil || *got.PeriodID != manual.Period.PeriodID {
		t.Fatalf("period of the manual payment = %v", got.PeriodID)
	}
}

func TestUserCardShowsPeriodsAndHistory(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := tgUser(t, s, 2001)
	p := mkPlan(t, s)
	vip := mkVisPlan(t, s, "vip", 99, model.VisibilityPrivate)
	now := time.Now().UTC()
	old := pay(t, s, u.ID, p.ID, now.Add(-40*day))
	if _, err := s.ExpireOverdue(ctx, now); err != nil {
		t.Fatal(err)
	}
	pp := mkProviderPayment(t, s, u.ID, p.ID)
	settled, err := s.SettlePayment(ctx, pp.PaymentID, now)
	if err != nil {
		t.Fatal(err)
	}
	pay(t, s, u.ID, p.ID, now)
	if err := s.GrantPlanAccess(ctx, vip.ID, u.ID, AccessAdmin, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterDevice(ctx, u.ID, DeviceInfo{HWID: "hw-1", UserAgent: "Happ/1", OS: "iOS", Model: "iPhone"}, nil); err != nil {
		t.Fatal(err)
	}
	card, err := s.UserCard(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.User.ID != u.ID || card.User.SubToken == "" {
		t.Fatalf("user = %+v", card.User)
	}
	if len(card.Periods) != 3 {
		t.Fatalf("periods = %+v", card.Periods)
	}
	if card.Periods[0].State != PeriodCurrent || card.Periods[0].ID != settled.Period.PeriodID || card.Periods[0].PaymentID == nil || *card.Periods[0].PaymentID != pp.PaymentID {
		t.Fatalf("current = %+v", card.Periods[0])
	}
	if card.Periods[1].State != PeriodQueued || card.Periods[2].State != PeriodClosed || card.Periods[2].ID != old.PeriodID {
		t.Fatalf("queued and closed = %+v / %+v", card.Periods[1], card.Periods[2])
	}
	if card.Periods[0].PlanName != p.Name {
		t.Fatalf("plan name = %q", card.Periods[0].PlanName)
	}
	if len(card.Devices) != 1 || card.Devices[0].Model != "iPhone" {
		t.Fatalf("devices = %+v", card.Devices)
	}
	if len(card.Access) != 1 || card.Access[0].PlanID != vip.ID || card.Access[0].PlanName != "vip" {
		t.Fatalf("access = %+v", card.Access)
	}
	if len(card.Payments) != 1 || card.Payments[0].PaymentID != pp.PaymentID || card.Payments[0].PlanName != p.Name {
		t.Fatalf("payments = %+v", card.Payments)
	}
	if card.Trial != nil {
		t.Fatalf("trial = %+v", card.Trial)
	}
	if _, err := s.UserCard(ctx, 999999); err != ErrNotFound {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestListPaymentsFilters(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := tgUser(t, s, 3001)
	other := tgUser(t, s, 3002)
	p := mkPlan(t, s)
	fresh := mkProviderPayment(t, s, u.ID, p.ID)
	stale := mkProviderPayment(t, s, other.ID, p.ID)
	if _, err := s.pool.Exec(ctx, `UPDATE payments SET created_at = now() - interval '2 days' WHERE payment_id = $1`, stale.PaymentID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordManualPayment(ctx, &model.Payment{PaymentID: "m-1", UserID: u.ID, PlanID: p.ID, AmountRub: 5}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	list := func(f PaymentFilter) []string {
		t.Helper()
		rows, err := s.ListPayments(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, r := range rows {
			out = append(out, r.PaymentID)
		}
		return out
	}
	if got := list(PaymentFilter{Limit: 50}); len(got) != 3 || got[0] != "m-1" {
		t.Fatalf("all = %v", got)
	}
	if got := list(PaymentFilter{Status: "pending", Limit: 50}); len(got) != 2 {
		t.Fatalf("pending = %v", got)
	}
	if got := list(PaymentFilter{Source: "manual", Limit: 50}); len(got) != 1 || got[0] != "m-1" {
		t.Fatalf("manual = %v", got)
	}
	if got := list(PaymentFilter{UserID: &other.ID, Limit: 50}); len(got) != 1 || got[0] != stale.PaymentID {
		t.Fatalf("by user = %v", got)
	}
	if got := list(PaymentFilter{StuckBefore: time.Now().Add(-24 * time.Hour), Limit: 50}); len(got) != 1 || got[0] != stale.PaymentID {
		t.Fatalf("stuck = %v", got)
	}
	page, err := s.ListPayments(ctx, PaymentFilter{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := list(PaymentFilter{Limit: 5, Before: page[0].ID}); len(got) != 2 || got[0] != stale.PaymentID {
		t.Fatalf("after the cursor = %v (fresh %s)", got, fresh.PaymentID)
	}
	rows, _ := s.ListPayments(ctx, PaymentFilter{UserID: &u.ID, Source: "yookassa", Limit: 5})
	if len(rows) != 1 || rows[0].TelegramID == nil || *rows[0].TelegramID != 3001 || rows[0].PlanName != p.Name {
		t.Fatalf("row = %+v", rows)
	}
}
