package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func (s *Store) CreateSubscription(ctx context.Context, sub *model.Subscription) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		sub.UserID, sub.PlanID, sub.StartsAt, sub.ExpiresAt,
	).Scan(&sub.ID, &sub.CreatedAt)
}

func (s *Store) GetActiveSubscription(ctx context.Context, userID int64) (*model.Subscription, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, plan_id, starts_at, expires_at, canceled, traffic_used_bytes, created_at
		 FROM subscriptions WHERE user_id = $1 AND NOT canceled AND expires_at > now()
		 ORDER BY expires_at DESC LIMIT 1`, userID)
	sub := &model.Subscription{}
	err := row.Scan(&sub.ID, &sub.UserID, &sub.PlanID, &sub.StartsAt, &sub.ExpiresAt, &sub.Canceled, &sub.TrafficUsedBytes, &sub.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func (s *Store) RenewSubscription(ctx context.Context, userID int64, planID int64, now time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var subID int64
	var currentExp time.Time
	err = tx.QueryRow(ctx,
		`SELECT id, expires_at FROM subscriptions
		 WHERE user_id = $1 AND NOT canceled AND expires_at > $2
		 ORDER BY expires_at DESC LIMIT 1 FOR UPDATE`,
		userID, now).Scan(&subID, &currentExp)
	if errors.Is(err, pgx.ErrNoRows) {
		var periodDays int
		if err := tx.QueryRow(ctx, `SELECT period_days FROM plans WHERE id = $1`, planID).Scan(&periodDays); err != nil {
			return 0, err
		}
		exp := now.AddDate(0, 0, periodDays)
		if err := tx.QueryRow(ctx,
			`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) VALUES ($1, $2, $3, $4) RETURNING id`,
			userID, planID, now, exp).Scan(&subID); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	} else {
		var periodDays int
		if err := tx.QueryRow(ctx, `SELECT period_days FROM plans WHERE id = $1`, planID).Scan(&periodDays); err != nil {
			return 0, err
		}
		newExp := currentExp.AddDate(0, 0, periodDays)
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET expires_at = $2 WHERE id = $1`, subID, newExp); err != nil {
			return 0, err
		}
	}
	return subID, tx.Commit(ctx)
}

func (s *Store) CancelSubscription(ctx context.Context, subID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE subscriptions SET canceled = TRUE WHERE id = $1`, subID)
	return err
}

func (s *Store) ExpireOverdue(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE subscriptions SET canceled = TRUE WHERE NOT canceled AND expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) AddTrafficUsed(ctx context.Context, userID int64, deltaBytes int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscriptions SET traffic_used_bytes = traffic_used_bytes + $2
		 WHERE id = (SELECT id FROM subscriptions WHERE user_id = $1 AND NOT canceled ORDER BY expires_at DESC LIMIT 1)`,
		userID, deltaBytes)
	return err
}

func (s *Store) CreatePayment(ctx context.Context, p *model.Payment) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		p.PaymentID, p.UserID, p.PlanID, p.AmountRub, p.Status,
	).Scan(&p.ID, &p.CreatedAt)
}

func (s *Store) GetPayment(ctx context.Context, paymentID string) (*model.Payment, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, payment_id, user_id, plan_id, amount_rub, status, created_at
		 FROM payments WHERE payment_id = $1`, paymentID)
	p := &model.Payment{}
	err := row.Scan(&p.ID, &p.PaymentID, &p.UserID, &p.PlanID, &p.AmountRub, &p.Status, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) SetPaymentStatus(ctx context.Context, paymentID string, status string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE payments SET status = $2 WHERE payment_id = $1`, paymentID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
