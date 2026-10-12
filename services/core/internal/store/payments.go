package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

type SettleResult struct {
	Applied bool
	Payment model.Payment
	Period  PaymentResult
}

const paymentColumns = `id, payment_id, user_id, plan_id, amount_rub, status, source, coalesce(confirmation_url, ''), paid_at, created_at`

func scanPayment(row pgxRow) (*model.Payment, error) {
	p := &model.Payment{}
	err := row.Scan(&p.ID, &p.PaymentID, &p.UserID, &p.PlanID, &p.AmountRub, &p.Status, &p.Source, &p.ConfirmationURL, &p.PaidAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) CreatePayment(ctx context.Context, p *model.Payment) error {
	if p.Source == "" {
		p.Source = "manual"
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status, source, confirmation_url)
		 VALUES ($1, $2, $3, $4, $5, $6, nullif($7, '')) RETURNING id, created_at`,
		p.PaymentID, p.UserID, p.PlanID, p.AmountRub, p.Status, p.Source, p.ConfirmationURL,
	).Scan(&p.ID, &p.CreatedAt)
}

func (s *Store) GetPayment(ctx context.Context, paymentID string) (*model.Payment, error) {
	return scanPayment(s.pool.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE payment_id = $1`, paymentID))
}

func (s *Store) SettlePayment(ctx context.Context, paymentID string, now time.Time) (SettleResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SettleResult{}, err
	}
	defer tx.Rollback(ctx)
	res, err := settleTx(ctx, tx, paymentID, now)
	if err != nil {
		return SettleResult{}, err
	}
	return res, tx.Commit(ctx)
}

func (s *Store) RecordManualPayment(ctx context.Context, p *model.Payment, now time.Time) (SettleResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SettleResult{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status, source)
		 VALUES ($1, $2, $3, $4, 'pending', 'manual') ON CONFLICT (payment_id) DO NOTHING`,
		p.PaymentID, p.UserID, p.PlanID, p.AmountRub); err != nil {
		return SettleResult{}, mapWriteErr(err)
	}
	var userID, planID, amount int64
	var source string
	if err := tx.QueryRow(ctx, `SELECT user_id, plan_id, amount_rub, source FROM payments WHERE payment_id = $1 FOR UPDATE`, p.PaymentID).Scan(&userID, &planID, &amount, &source); err != nil {
		return SettleResult{}, err
	}
	if source != "manual" || userID != p.UserID || planID != p.PlanID || amount != p.AmountRub {
		return SettleResult{}, ErrConflict
	}
	res, err := settleTx(ctx, tx, p.PaymentID, now)
	if err != nil {
		return SettleResult{}, err
	}
	return res, tx.Commit(ctx)
}

func settleTx(ctx context.Context, tx pgx.Tx, paymentID string, now time.Time) (SettleResult, error) {
	p, err := scanPayment(tx.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE payment_id = $1 FOR UPDATE`, paymentID))
	if err != nil {
		return SettleResult{}, err
	}
	if p.Status != "pending" {
		return SettleResult{Payment: *p}, nil
	}
	period, err := applyPaymentTx(ctx, tx, p.UserID, p.PlanID, now)
	if err != nil {
		return SettleResult{}, err
	}
	if err := tx.QueryRow(ctx,
		`UPDATE payments SET status = 'succeeded', paid_at = $2 WHERE id = $1 RETURNING status, paid_at`,
		p.ID, now).Scan(&p.Status, &p.PaidAt); err != nil {
		return SettleResult{}, err
	}
	return SettleResult{Applied: true, Payment: *p, Period: period}, nil
}

func (s *Store) FinishPayment(ctx context.Context, paymentID string, status string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE payments SET status = $2 WHERE payment_id = $1 AND status = 'pending'`, paymentID, status)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) FindPendingPayment(ctx context.Context, userID, planID, amountRub int64, since time.Time) (*model.Payment, error) {
	return scanPayment(s.pool.QueryRow(ctx,
		`SELECT `+paymentColumns+` FROM payments
		 WHERE user_id = $1 AND plan_id = $2 AND amount_rub = $3 AND status = 'pending' AND source = 'yookassa' AND created_at > $4
		 ORDER BY created_at DESC LIMIT 1`, userID, planID, amountRub, since))
}

func (s *Store) ListPendingPayments(ctx context.Context, since time.Time, limit int) ([]model.Payment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+paymentColumns+` FROM payments
		 WHERE status = 'pending' AND source = 'yookassa' AND created_at > $1
		 ORDER BY created_at DESC, id DESC LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}
