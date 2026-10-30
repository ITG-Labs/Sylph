package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

var (
	ErrUserBanned     = errors.New("user is banned")
	ErrNoActivePeriod = errors.New("user has no active period")
	ErrUnlimitedPlan  = errors.New("plan has no traffic limit")
	ErrPoolNotInPlan  = errors.New("pool is not part of the current plan")
	ErrPeriodClosed   = errors.New("period is already closed")
)

func (s *Store) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockUserStatus(ctx context.Context, tx pgx.Tx, userID int64) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id = $1 FOR NO KEY UPDATE`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return status, err
}

func lockActiveUser(ctx context.Context, tx pgx.Tx, userID int64) error {
	status, err := lockUserStatus(ctx, tx, userID)
	if err != nil {
		return err
	}
	if status == model.UserBanned {
		return ErrUserBanned
	}
	return nil
}

func lockCurrent(ctx context.Context, tx pgx.Tx, userID int64, now time.Time) (*period, []period, error) {
	if err := lockActiveUser(ctx, tx, userID); err != nil {
		return nil, nil, err
	}
	timeline, err := lockTimeline(ctx, tx, userID, now)
	if err != nil {
		return nil, nil, err
	}
	if len(timeline) == 0 || timeline[0].startsAt.After(now) {
		return nil, nil, ErrNoActivePeriod
	}
	return &timeline[0], timeline, nil
}

func (s *Store) AddTrafficBonus(ctx context.Context, userID int64, poolID *int64, bytes int64, now time.Time) (*model.Subscription, error) {
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		cur, _, err := lockCurrent(ctx, tx, userID, now)
		if err != nil {
			return err
		}
		if poolID != nil {
			return addPoolBonus(ctx, tx, cur, *poolID, bytes)
		}
		if cur.limit == nil {
			return ErrUnlimitedPlan
		}
		_, err = tx.Exec(ctx,
			`UPDATE subscriptions SET traffic_bonus_bytes = LEAST(traffic_bonus_bytes::numeric + $2, 9223372036854775807)::bigint WHERE id = $1`,
			cur.id, bytes)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetActiveSubscription(ctx, userID)
}

func addPoolBonus(ctx context.Context, tx pgx.Tx, cur *period, poolID, bytes int64) error {
	var limit *int64
	err := tx.QueryRow(ctx, `SELECT traffic_limit_bytes FROM plan_pools WHERE id = $1 AND plan_id = $2`, poolID, cur.planID).Scan(&limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPoolNotInPlan
	}
	if err != nil {
		return err
	}
	if limit == nil {
		return ErrUnlimitedPlan
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO period_pool_usage (period_id, pool_id, bonus_bytes) VALUES ($1, $2, $3)
		 ON CONFLICT (period_id, pool_id) DO UPDATE
		 SET bonus_bytes = LEAST(period_pool_usage.bonus_bytes::numeric + EXCLUDED.bonus_bytes, 9223372036854775807)::bigint`,
		cur.id, poolID, bytes)
	return err
}

func (s *Store) ExtendPeriod(ctx context.Context, userID int64, days int, now time.Time) (*model.Subscription, error) {
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		cur, timeline, err := lockCurrent(ctx, tx, userID, now)
		if err != nil {
			return err
		}
		ids := make([]int64, 0, len(timeline))
		for _, p := range timeline {
			ids = append(ids, p.id)
		}
		_, err = tx.Exec(ctx,
			`UPDATE subscriptions
			 SET starts_at = CASE WHEN id = $2 THEN starts_at ELSE starts_at + $3::int * interval '1 day' END,
			     expires_at = expires_at + $3::int * interval '1 day'
			 WHERE id = ANY($1)`, ids, cur.id, days)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetActiveSubscription(ctx, userID)
}

func (s *Store) ChangePeriodPlan(ctx context.Context, userID, planID int64, now time.Time) (bool, error) {
	changed := false
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		cur, _, err := lockCurrent(ctx, tx, userID, now)
		if err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM plans WHERE id = $1)`, planID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if cur.planID == planID {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET plan_id = $2 WHERE id = $1`, cur.id, planID); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

func (s *Store) ClosePeriod(ctx context.Context, periodID int64, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		closed, err := closePeriodTx(ctx, tx, periodID, now)
		if err != nil {
			return err
		}
		if !closed {
			return ErrPeriodClosed
		}
		return nil
	})
}

func closePeriodTx(ctx context.Context, tx pgx.Tx, periodID int64, now time.Time) (bool, error) {
	var userID int64
	err := tx.QueryRow(ctx, `SELECT user_id FROM subscriptions WHERE id = $1`, periodID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if _, err := lockUserStatus(ctx, tx, userID); err != nil {
		return false, err
	}
	timeline, err := lockTimeline(ctx, tx, userID, now)
	if err != nil {
		return false, err
	}
	idx := -1
	for i, p := range timeline {
		if p.id == periodID {
			idx = i
		}
	}
	if idx < 0 {
		return false, nil
	}
	p := timeline[idx]
	var gap time.Duration
	if p.startsAt.After(now) {
		gap = p.expires.Sub(p.startsAt)
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET canceled = TRUE, closed_manually = TRUE WHERE id = $1`, p.id); err != nil {
			return false, err
		}
	} else {
		gap = p.expires.Sub(now)
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET canceled = TRUE, closed_manually = TRUE, expires_at = $2 WHERE id = $1`, p.id, now); err != nil {
			return false, err
		}
	}
	for _, q := range timeline[idx+1:] {
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET starts_at = $2, expires_at = $3 WHERE id = $1`,
			q.id, q.startsAt.Add(-gap), q.expires.Add(-gap)); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Store) BanUser(ctx context.Context, userID int64, reason string, now time.Time) (bool, error) {
	already := false
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		status, err := lockUserStatus(ctx, tx, userID)
		if err != nil {
			return err
		}
		if status == model.UserBanned {
			already = true
			return nil
		}
		if _, err := tx.Exec(ctx,
			`UPDATE subscriptions SET canceled = TRUE, closed_manually = TRUE, expires_at = CASE WHEN starts_at <= $2 THEN $2 ELSE expires_at END
			 WHERE user_id = $1 AND NOT canceled AND expires_at > $2`, userID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET status = 'banned', banned_at = $2, ban_reason = nullif($3, '') WHERE id = $1`, userID, now, reason); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE plan_access SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL`, userID, now)
		return err
	})
	return already, err
}

func (s *Store) UnbanUser(ctx context.Context, userID int64) (bool, error) {
	already := false
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		status, err := lockUserStatus(ctx, tx, userID)
		if err != nil {
			return err
		}
		if status != model.UserBanned {
			already = true
			return nil
		}
		_, err = tx.Exec(ctx, `UPDATE users SET status = 'active', banned_at = NULL, ban_reason = NULL WHERE id = $1`, userID)
		return err
	})
	return already, err
}
