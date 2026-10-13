package store

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

var ErrPaymentState = errors.New("payment is in another status")

type PaymentStateError struct {
	Status string
}

func (e *PaymentStateError) Error() string {
	return "payment is " + e.Status
}

func (e *PaymentStateError) Unwrap() error {
	return ErrPaymentState
}

func lockPayment(ctx context.Context, tx pgx.Tx, paymentID string, allowed ...string) (*model.Payment, error) {
	p, err := scanPayment(tx.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE payment_id = $1 FOR UPDATE`, paymentID))
	if err != nil {
		return nil, err
	}
	if !slices.Contains(allowed, p.Status) {
		return nil, &PaymentStateError{Status: p.Status}
	}
	return p, nil
}

func (s *Store) ApplyMismatchPayment(ctx context.Context, paymentID, note string, now time.Time) (SettleResult, error) {
	var res SettleResult
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		p, err := lockPayment(ctx, tx, paymentID, "mismatch")
		if err != nil {
			return err
		}
		if err := lockActiveUser(ctx, tx, p.UserID); err != nil {
			return err
		}
		period, err := applyPaymentTx(ctx, tx, p.UserID, p.PlanID, now)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`UPDATE payments SET status = 'succeeded', paid_at = $2, period_id = $3, note = $4 WHERE id = $1
			 RETURNING status, paid_at, period_id, note`, p.ID, now, period.PeriodID, note).
			Scan(&p.Status, &p.PaidAt, &p.PeriodID, &p.Note); err != nil {
			return err
		}
		res = SettleResult{Applied: true, Payment: *p, Period: period}
		return nil
	})
	return res, err
}

func (s *Store) RejectPayment(ctx context.Context, paymentID, note string, from ...string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		p, err := lockPayment(ctx, tx, paymentID, from...)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE payments SET status = 'rejected', note = $2 WHERE id = $1`, p.ID, note)
		return err
	})
}

func (s *Store) RefundPayment(ctx context.Context, paymentID, note string, now time.Time) (bool, error) {
	closed := false
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		p, err := lockPayment(ctx, tx, paymentID, "succeeded")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'refunded', refunded_at = $2, note = $3 WHERE id = $1`, p.ID, now, note); err != nil {
			return err
		}
		if p.PeriodID == nil {
			return nil
		}
		closed, err = closePeriodTx(ctx, tx, *p.PeriodID, now)
		return err
	})
	return closed, err
}
