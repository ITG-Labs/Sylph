package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Service) GrantTrial(ctx context.Context, userID int64) (store.TrialResult, error) {
	res, err := s.store.GrantTrial(ctx, userID, time.Now().UTC())
	if err != nil {
		return res, err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "trial_granted",
		slog.Int64("user_id", userID),
		slog.Int64("plan_id", res.PlanID),
		slog.Int64("period_id", res.PeriodID))
	return res, nil
}
