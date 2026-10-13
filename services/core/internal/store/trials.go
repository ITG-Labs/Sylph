package store

import (
	"context"
	"errors"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

var ErrTrialUnavailable = errors.New("trial unavailable")

const visibleToUser = `(p.visibility = 'public' OR EXISTS (SELECT 1 FROM plan_access a WHERE a.plan_id = p.id AND a.user_id = $1 AND a.revoked_at IS NULL))`

const trialOffer = `SELECT ` + planColumnsP + ` FROM plans p
	WHERE p.trial AND p.on_sale AND ` + visibleToUser + `
	  AND NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_id = $1)
	  AND NOT EXISTS (SELECT 1 FROM trials t WHERE t.user_id = $1)`

type TrialResult struct {
	PlanID    int64
	PeriodID  int64
	ExpiresAt time.Time
}

func (s *Store) TrialPlanFor(ctx context.Context, userID int64) (*model.Plan, error) {
	p, err := s.scanPlan(s.pool.QueryRow(ctx, trialOffer, userID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return p, err
}

func (s *Store) GrantTrial(ctx context.Context, userID int64, now time.Time) (TrialResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TrialResult{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockActiveUser(ctx, tx, userID); err != nil {
		return TrialResult{}, err
	}
	plan, err := s.scanPlan(tx.QueryRow(ctx, trialOffer, userID))
	if errors.Is(err, ErrNotFound) {
		return TrialResult{}, ErrTrialUnavailable
	}
	if err != nil {
		return TrialResult{}, err
	}
	res, err := applyPaymentTx(ctx, tx, userID, plan.ID, now)
	if err != nil {
		return TrialResult{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trials (user_id, plan_id, period_id, granted_at) VALUES ($1, $2, $3, $4)`,
		userID, plan.ID, res.PeriodID, now); err != nil {
		return TrialResult{}, err
	}
	var expires time.Time
	if err := tx.QueryRow(ctx, `SELECT expires_at FROM subscriptions WHERE id = $1`, res.PeriodID).Scan(&expires); err != nil {
		return TrialResult{}, err
	}
	return TrialResult{PlanID: plan.ID, PeriodID: res.PeriodID, ExpiresAt: expires}, tx.Commit(ctx)
}
