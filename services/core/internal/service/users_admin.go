package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	maxReasonRunes = 256
	maxExtendDays  = 3650
	maxBonusBytes  = 1 << 50
)

func (s *Service) activeUser(ctx context.Context, userID int64) (*model.User, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.Status == model.UserBanned {
		return nil, store.ErrUserBanned
	}
	return u, nil
}

func adminErr(err error) error {
	switch {
	case errors.Is(err, store.ErrUserBanned):
		return &ConflictError{Field: "user_id", Message: "user is banned"}
	case errors.Is(err, store.ErrNoActivePeriod):
		return &ConflictError{Field: "subscription", Message: "user has no active period"}
	case errors.Is(err, store.ErrUnlimitedPlan):
		return &ConflictError{Field: "bytes", Message: "plan of the current period has no traffic limit"}
	case errors.Is(err, store.ErrPeriodClosed):
		return &ConflictError{Field: "id", Message: "period is already closed or over"}
	}
	return err
}

func (s *Service) BanUser(ctx context.Context, userID int64, reason string) (bool, error) {
	if err := validText("reason", reason, maxReasonRunes); err != nil {
		return false, err
	}
	already, err := s.store.BanUser(ctx, userID, reason, time.Now().UTC())
	if err != nil {
		return false, err
	}
	if !already {
		s.log.LogAttrs(ctx, slog.LevelInfo, "user_banned", slog.Int64("user_id", userID))
	}
	return already, nil
}

func (s *Service) UnbanUser(ctx context.Context, userID int64) (bool, error) {
	already, err := s.store.UnbanUser(ctx, userID)
	if err != nil {
		return false, err
	}
	if !already {
		s.log.LogAttrs(ctx, slog.LevelInfo, "user_unbanned", slog.Int64("user_id", userID))
	}
	return already, nil
}

func (s *Service) ExtendSubscription(ctx context.Context, userID int64, days int) (*model.Subscription, error) {
	if days < 1 || days > maxExtendDays {
		return nil, fieldErr("days", "must be 1-3650")
	}
	sub, err := s.store.ExtendPeriod(ctx, userID, days, time.Now().UTC())
	if err != nil {
		return nil, adminErr(err)
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "subscription_extended", slog.Int64("user_id", userID), slog.Int("days", days))
	return sub, nil
}

func (s *Service) AddTraffic(ctx context.Context, userID, bytes int64) (*model.Subscription, error) {
	if bytes < 1 || bytes > maxBonusBytes {
		return nil, fieldErr("bytes", "must be 1 byte to 1 PiB")
	}
	sub, err := s.store.AddTrafficBonus(ctx, userID, bytes, time.Now().UTC())
	if err != nil {
		return nil, adminErr(err)
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "traffic_bonus_added", slog.Int64("user_id", userID), slog.Int64("bytes", bytes))
	return sub, nil
}

func (s *Service) ChangeSubscriptionPlan(ctx context.Context, userID, planID int64) (bool, error) {
	if planID <= 0 {
		return false, fieldErr("plan_id", "must be positive")
	}
	changed, err := s.store.ChangePeriodPlan(ctx, userID, planID, time.Now().UTC())
	if err != nil {
		return false, adminErr(err)
	}
	if changed {
		s.log.LogAttrs(ctx, slog.LevelInfo, "subscription_plan_changed", slog.Int64("user_id", userID), slog.Int64("plan_id", planID))
	}
	return changed, nil
}

func (s *Service) ClosePeriod(ctx context.Context, periodID int64) error {
	if err := s.store.ClosePeriod(ctx, periodID, time.Now().UTC()); err != nil {
		return adminErr(err)
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "period_closed", slog.Int64("period_id", periodID))
	return nil
}
