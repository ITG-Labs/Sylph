package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkProviderPayment(t *testing.T, s *Store, userID, planID int64) *model.Payment {
	t.Helper()
	p := &model.Payment{PaymentID: model.NewUUID(), UserID: userID, PlanID: planID, AmountRub: 100, Status: "pending", Source: "yookassa", ConfirmationURL: "https://pay.example/1"}
	if err := s.CreatePayment(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func paymentRow(t *testing.T, s *Store, paymentID string) *model.Payment {
	t.Helper()
	p, err := s.GetPayment(context.Background(), paymentID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSettlePaymentAppliesOnceUnderConcurrency(t *testing.T) {
	s := setupStore(t)
	u, p := mkUser(t, s), mkPlan(t, s)
	pm := mkProviderPayment(t, s, u.ID, p.ID)
	var wg sync.WaitGroup
	results := make([]SettleResult, 8)
	errs := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = s.SettlePayment(context.Background(), pm.PaymentID, time.Now().UTC())
		}()
	}
	wg.Wait()
	applied := 0
	for i, r := range results {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if r.Applied {
			applied++
		}
		if r.Payment.Status != "succeeded" {
			t.Fatalf("settle %d sees status %q", i, r.Payment.Status)
		}
	}
	if applied != 1 {
		t.Fatalf("applied %d times, want exactly once", applied)
	}
	if ps := userPeriods(t, s, u.ID); len(ps) != 1 {
		t.Fatalf("periods = %d, want 1", len(ps))
	}
	if got := paymentRow(t, s, pm.PaymentID); got.Status != "succeeded" || got.PaidAt == nil {
		t.Fatalf("payment after settle = %+v", got)
	}
}

func TestSettlePaymentIsAtomicWithThePeriod(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, p := mkUser(t, s), mkPlan(t, s)
	pm := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.pool.Exec(ctx, `
		CREATE FUNCTION fail_period() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'boom'; END $$ LANGUAGE plpgsql;
		CREATE TRIGGER trg_fail_period BEFORE INSERT ON subscriptions FOR EACH ROW EXECUTE FUNCTION fail_period();`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettlePayment(ctx, pm.PaymentID, time.Now().UTC()); err == nil {
		t.Fatal("settle must fail when the period cannot be written")
	}
	if got := paymentRow(t, s, pm.PaymentID); got.Status != "pending" || got.PaidAt != nil {
		t.Fatalf("payment marked %q without a period", got.Status)
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER trg_fail_period ON subscriptions`); err != nil {
		t.Fatal(err)
	}
	res, err := s.SettlePayment(ctx, pm.PaymentID, time.Now().UTC())
	if err != nil || !res.Applied {
		t.Fatalf("retry after the failure = %+v, %v", res, err)
	}
}

func TestSettlePaymentSkipsFinalStatuses(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, p := mkUser(t, s), mkPlan(t, s)
	for _, status := range []string{"canceled", "mismatch"} {
		pm := mkProviderPayment(t, s, u.ID, p.ID)
		ok, err := s.FinishPayment(ctx, pm.PaymentID, status)
		if err != nil || !ok {
			t.Fatalf("finish %s = %v, %v", status, ok, err)
		}
		res, err := s.SettlePayment(ctx, pm.PaymentID, time.Now().UTC())
		if err != nil || res.Applied || res.Payment.Status != status {
			t.Fatalf("settle of %s payment = %+v, %v", status, res, err)
		}
	}
	if ps := userPeriods(t, s, u.ID); len(ps) != 0 {
		t.Fatalf("final payments created periods: %+v", ps)
	}
}

func TestFinishPaymentOnlyMovesPending(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, p := mkUser(t, s), mkPlan(t, s)
	pm := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.SettlePayment(ctx, pm.PaymentID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	ok, err := s.FinishPayment(ctx, pm.PaymentID, "canceled")
	if err != nil || ok {
		t.Fatalf("finish of a succeeded payment = %v, %v", ok, err)
	}
	if got := paymentRow(t, s, pm.PaymentID); got.Status != "succeeded" {
		t.Fatalf("succeeded payment became %q", got.Status)
	}
}

func TestSettleUnknownPaymentIsNotFound(t *testing.T) {
	s := setupStore(t)
	if _, err := s.SettlePayment(context.Background(), "nope", time.Now().UTC()); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestManualPaymentRepeatedConcurrentlyAppliesOnce(t *testing.T) {
	s := setupStore(t)
	u, p := mkUser(t, s), mkPlan(t, s)
	var wg sync.WaitGroup
	var mu sync.Mutex
	applied := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := s.RecordManualPayment(context.Background(), &model.Payment{PaymentID: "manual-1", UserID: u.ID, PlanID: p.ID, AmountRub: 100}, time.Now().UTC())
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if res.Applied {
				applied++
			}
		}()
	}
	wg.Wait()
	if applied != 1 {
		t.Fatalf("manual payment applied %d times", applied)
	}
	if ps := userPeriods(t, s, u.ID); len(ps) != 1 {
		t.Fatalf("periods = %d, want 1", len(ps))
	}
	if got := paymentRow(t, s, "manual-1"); got.Source != "manual" || got.Status != "succeeded" {
		t.Fatalf("manual payment = %+v", got)
	}
}

func TestSettleWaitsForRotationWithoutDeadlock(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, p := mkUser(t, s), mkPlan(t, s)
	pm := mkProviderPayment(t, s, u.ID, p.ID)
	done := make(chan struct{})
	var rotErr, setErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 20 {
			if _, err := s.RotateUserCredentials(ctx, u.ID, model.NewToken()); err != nil {
				rotErr = err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		_, setErr = s.SettlePayment(ctx, pm.PaymentID, time.Now().UTC())
	}()
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("rotation and settle are stuck")
	}
	if rotErr != nil || setErr != nil {
		t.Fatalf("rotate: %v, settle: %v", rotErr, setErr)
	}
}

func TestFindPendingPaymentForReuse(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, p := mkUser(t, s), mkPlan(t, s)
	other := mkPlan(t, s)
	pm := mkProviderPayment(t, s, u.ID, p.ID)
	got, err := s.FindPendingPayment(ctx, u.ID, p.ID, 100, time.Now().Add(-time.Minute))
	if err != nil || got.PaymentID != pm.PaymentID || got.ConfirmationURL != pm.ConfirmationURL {
		t.Fatalf("reuse = %+v, %v", got, err)
	}
	for name, q := range map[string]func() error{
		"other plan": func() error {
			_, err := s.FindPendingPayment(ctx, u.ID, other.ID, 100, time.Now().Add(-time.Minute))
			return err
		},
		"other amount": func() error {
			_, err := s.FindPendingPayment(ctx, u.ID, p.ID, 150, time.Now().Add(-time.Minute))
			return err
		},
		"too old": func() error {
			_, err := s.FindPendingPayment(ctx, u.ID, p.ID, 100, time.Now().Add(time.Minute))
			return err
		},
	} {
		if err := q(); err != ErrNotFound {
			t.Fatalf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
	if _, err := s.SettlePayment(ctx, pm.PaymentID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindPendingPayment(ctx, u.ID, p.ID, 100, time.Now().Add(-time.Minute)); err != ErrNotFound {
		t.Fatalf("settled payment offered for reuse: %v", err)
	}
	manual := &model.Payment{PaymentID: "m-pending", UserID: u.ID, PlanID: p.ID, AmountRub: 100, Status: "pending", Source: "manual"}
	if err := s.CreatePayment(ctx, manual); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindPendingPayment(ctx, u.ID, p.ID, 100, time.Now().Add(-time.Minute)); err != ErrNotFound {
		t.Fatalf("manual payment offered for reuse: %v", err)
	}
}

func TestListPendingPaymentsNewestFirstWithinWindow(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, p := mkUser(t, s), mkPlan(t, s)
	a := mkProviderPayment(t, s, u.ID, p.ID)
	b := mkProviderPayment(t, s, u.ID, p.ID)
	old := mkProviderPayment(t, s, u.ID, p.ID)
	done := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.pool.Exec(ctx, `UPDATE payments SET created_at = now() - interval '2 hours' WHERE payment_id = $1`, a.PaymentID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE payments SET created_at = now() - interval '25 hours' WHERE payment_id = $1`, old.PaymentID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishPayment(ctx, done.PaymentID, "canceled"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePayment(ctx, &model.Payment{PaymentID: "m-1", UserID: u.ID, PlanID: p.ID, AmountRub: 1, Status: "pending", Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListPendingPayments(ctx, time.Now().Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].PaymentID != b.PaymentID || got[1].PaymentID != a.PaymentID {
		t.Fatalf("pending = %+v", got)
	}
	got, err = s.ListPendingPayments(ctx, time.Now().Add(-24*time.Hour), 1)
	if err != nil || len(got) != 1 || got[0].PaymentID != b.PaymentID {
		t.Fatalf("limit 1 = %+v, %v", got, err)
	}
}

func TestManualPaymentRefusesSomeoneElsesID(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, other, p := mkUser(t, s), mkUser(t, s), mkPlan(t, s)
	provider := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.RecordManualPayment(ctx, &model.Payment{PaymentID: provider.PaymentID, UserID: u.ID, PlanID: p.ID, AmountRub: 100}, time.Now().UTC()); err != ErrConflict {
		t.Fatalf("manual over a provider payment: %v", err)
	}
	if got := paymentRow(t, s, provider.PaymentID); got.Status != "pending" {
		t.Fatalf("provider payment touched: %q", got.Status)
	}
	if _, err := s.RecordManualPayment(ctx, &model.Payment{PaymentID: "m-1", UserID: u.ID, PlanID: p.ID, AmountRub: 100}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for name, pm := range map[string]*model.Payment{
		"other user":   {PaymentID: "m-1", UserID: other.ID, PlanID: p.ID, AmountRub: 100},
		"other amount": {PaymentID: "m-1", UserID: u.ID, PlanID: p.ID, AmountRub: 1},
	} {
		if _, err := s.RecordManualPayment(ctx, pm, time.Now().UTC()); err != ErrConflict {
			t.Fatalf("%s: err = %v, want ErrConflict", name, err)
		}
	}
	res, err := s.RecordManualPayment(ctx, &model.Payment{PaymentID: "m-1", UserID: u.ID, PlanID: p.ID, AmountRub: 100}, time.Now().UTC())
	if err != nil || res.Applied || res.Payment.Status != "succeeded" {
		t.Fatalf("exact repeat = %+v, %v", res, err)
	}
	if ps := userPeriods(t, s, other.ID); len(ps) != 0 {
		t.Fatalf("other user got a period: %+v", ps)
	}
}

func TestListPendingPaymentsOrdersLeastRecentlyPolledFirst(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, p := mkUser(t, s), mkPlan(t, s)
	a := mkProviderPayment(t, s, u.ID, p.ID)
	b := mkProviderPayment(t, s, u.ID, p.ID)
	c := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.pool.Exec(ctx, `UPDATE payments SET created_at = now() - interval '3 hours' WHERE payment_id = $1`, a.PaymentID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE payments SET created_at = now() - interval '1 hour' WHERE payment_id = $1`, b.PaymentID); err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-24 * time.Hour)
	ids := func(limit int) []string {
		t.Helper()
		got, err := s.ListPendingPayments(ctx, since, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, x := range got {
			out = append(out, x.PaymentID)
		}
		return out
	}
	if got := ids(10); len(got) != 3 || got[0] != c.PaymentID || got[1] != b.PaymentID || got[2] != a.PaymentID {
		t.Fatalf("never polled = %v, want newest first", got)
	}
	if err := s.MarkPaymentPolled(ctx, c.PaymentID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPaymentPolled(ctx, b.PaymentID); err != nil {
		t.Fatal(err)
	}
	if got := ids(1); len(got) != 1 || got[0] != a.PaymentID {
		t.Fatalf("after polling two = %v, want the one never polled", got)
	}
	if got := ids(10); got[0] != a.PaymentID || got[1] != c.PaymentID || got[2] != b.PaymentID {
		t.Fatalf("order = %v, want never polled, then the least recently polled", got)
	}
}

func mkTelegramUser(t *testing.T, s *Store, tg int64) *model.User {
	t.Helper()
	u := &model.User{TelegramID: &tg, SubToken: model.NewToken(), Status: "active"}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestUnnotifiedPaymentsQueue(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	p := mkPlan(t, s)
	u, noTG, banned, late := mkTelegramUser(t, s, 8001), mkUser(t, s), mkTelegramUser(t, s, 8002), mkTelegramUser(t, s, 8003)
	now := time.Now().UTC()
	settle := func(user *model.User, mutate func(*model.Payment)) *model.Payment {
		t.Helper()
		pm := mkProviderPayment(t, s, user.ID, p.ID)
		if mutate != nil {
			mutate(pm)
		}
		if _, err := s.SettlePayment(ctx, pm.PaymentID, now); err != nil {
			t.Fatal(err)
		}
		return pm
	}
	paid := settle(u, nil)
	settle(noTG, nil)
	settle(banned, nil)
	if _, err := s.BanUser(ctx, banned.ID, "", now); err != nil {
		t.Fatal(err)
	}
	mkProviderPayment(t, s, u.ID, p.ID)
	canceled := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.FinishPayment(ctx, canceled.PaymentID, "canceled"); err != nil {
		t.Fatal(err)
	}
	manual, err := s.RecordManualPayment(ctx, &model.Payment{PaymentID: "m-notify", UserID: u.ID, PlanID: p.ID, AmountRub: 100}, now)
	if err != nil || !manual.Applied {
		t.Fatalf("manual = %+v, %v", manual, err)
	}
	old := settle(late, nil)
	if _, err := s.pool.Exec(ctx, `UPDATE payments SET paid_at = now() - interval '10 days' WHERE payment_id = $1`, old.PaymentID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListUnnotifiedPayments(ctx, now.Add(-72*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PaymentID != paid.PaymentID || got[0].TelegramID != 8001 || got[0].UserID != u.ID {
		t.Fatalf("queue = %+v, want only the paid yookassa payment of a reachable user", got)
	}
	if err := s.MarkPaymentNotified(ctx, paid.PaymentID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPaymentNotified(ctx, paid.PaymentID); err != nil {
		t.Fatalf("second mark: %v", err)
	}
	if got, err := s.ListUnnotifiedPayments(ctx, now.Add(-72*time.Hour), 10); err != nil || len(got) != 0 {
		t.Fatalf("queue after mark = %+v, %v", got, err)
	}
	if err := s.MarkPaymentNotified(ctx, "no-such-payment"); err != ErrNotFound {
		t.Fatalf("unknown payment: %v", err)
	}
}

func TestSettledPaymentEntersTheNotifyQueueOnAnyPath(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u, p := mkTelegramUser(t, s, 8010), mkPlan(t, s)
	pm := mkProviderPayment(t, s, u.ID, p.ID)
	if _, err := s.SettlePayment(ctx, pm.PaymentID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListUnnotifiedPayments(ctx, time.Now().Add(-72*time.Hour), 10); len(got) != 1 {
		t.Fatalf("a payment settled by any path must be queued: %+v", got)
	}
}
