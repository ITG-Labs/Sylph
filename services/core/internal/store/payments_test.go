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

func TestListPendingPaymentsOldestFirstWithinWindow(t *testing.T) {
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
	if len(got) != 2 || got[0].PaymentID != a.PaymentID || got[1].PaymentID != b.PaymentID {
		t.Fatalf("pending = %+v", got)
	}
	got, err = s.ListPendingPayments(ctx, time.Now().Add(-24*time.Hour), 1)
	if err != nil || len(got) != 1 || got[0].PaymentID != a.PaymentID {
		t.Fatalf("limit 1 = %+v, %v", got, err)
	}
}
