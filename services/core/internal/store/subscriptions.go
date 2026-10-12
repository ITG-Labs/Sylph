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
		`SELECT id, user_id, plan_id, starts_at, expires_at, canceled, traffic_used_bytes, created_at,
		        (SELECT max(q.expires_at) FROM subscriptions q WHERE q.user_id = s.user_id AND NOT q.canceled AND q.expires_at > now())
		 FROM subscriptions s WHERE user_id = $1 AND NOT canceled AND starts_at <= now() AND expires_at > now()
		 ORDER BY starts_at DESC, id DESC LIMIT 1`, userID)
	sub := &model.Subscription{}
	err := row.Scan(&sub.ID, &sub.UserID, &sub.PlanID, &sub.StartsAt, &sub.ExpiresAt, &sub.Canceled, &sub.TrafficUsedBytes, &sub.CreatedAt, &sub.PaidUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

type PaymentResult struct {
	PeriodID  int64
	StartsNow bool
}

type period struct {
	id       int64
	planID   int64
	startsAt time.Time
	expires  time.Time
	used     int64
	limit    *int64
}

func (s *Store) ApplyPayment(ctx context.Context, userID int64, planID int64, now time.Time) (PaymentResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentResult{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, userID); err != nil {
		return PaymentResult{}, err
	}
	var periodDays int
	if err := tx.QueryRow(ctx, `SELECT period_days FROM plans WHERE id = $1`, planID).Scan(&periodDays); err != nil {
		return PaymentResult{}, err
	}
	timeline, err := lockTimeline(ctx, tx, userID, now)
	if err != nil {
		return PaymentResult{}, err
	}
	var current *period
	queue := timeline
	if len(timeline) > 0 && !timeline[0].startsAt.After(now) {
		current, queue = &timeline[0], timeline[1:]
	}
	exhausted := current != nil && current.limit != nil && current.used >= *current.limit
	if current != nil && !exhausted && current.planID == planID {
		start := timeline[len(timeline)-1].expires
		id, err := insertPeriod(ctx, tx, userID, planID, start, start.AddDate(0, 0, periodDays))
		if err != nil {
			return PaymentResult{}, err
		}
		return PaymentResult{PeriodID: id}, tx.Commit(ctx)
	}
	if current != nil {
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET canceled = TRUE, expires_at = $2 WHERE id = $1`, current.id, now); err != nil {
			return PaymentResult{}, err
		}
	}
	cursor := now.AddDate(0, 0, periodDays)
	id, err := insertPeriod(ctx, tx, userID, planID, now, cursor)
	if err != nil {
		return PaymentResult{}, err
	}
	for _, q := range queue {
		next := cursor.Add(q.expires.Sub(q.startsAt))
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET starts_at = $2, expires_at = $3 WHERE id = $1`, q.id, cursor, next); err != nil {
			return PaymentResult{}, err
		}
		cursor = next
	}
	return PaymentResult{PeriodID: id, StartsNow: true}, tx.Commit(ctx)
}

func lockTimeline(ctx context.Context, tx pgx.Tx, userID int64, now time.Time) ([]period, error) {
	rows, err := tx.Query(ctx,
		`SELECT s.id, s.plan_id, s.starts_at, s.expires_at, s.traffic_used_bytes, p.traffic_limit_bytes
		 FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		 WHERE s.user_id = $1 AND NOT s.canceled AND s.expires_at > $2
		 ORDER BY s.starts_at, s.id FOR UPDATE OF s`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []period
	for rows.Next() {
		var p period
		if err := rows.Scan(&p.id, &p.planID, &p.startsAt, &p.expires, &p.used, &p.limit); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func insertPeriod(ctx context.Context, tx pgx.Tx, userID, planID int64, starts, expires time.Time) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, planID, starts, expires).Scan(&id)
	return id, err
}

func (s *Store) CancelSubscription(ctx context.Context, subID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE subscriptions SET canceled = TRUE WHERE id = $1`, subID)
	return err
}

func (s *Store) ExpireOverdue(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM subscriptions WHERE NOT canceled AND expires_at <= $1 FOR UPDATE SKIP LOCKED`, now)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := tx.Exec(ctx, `UPDATE subscriptions SET canceled = TRUE WHERE id = ANY($1)`, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
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
