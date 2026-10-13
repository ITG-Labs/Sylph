package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Service) RegisterUser(ctx context.Context, telegramID int64) (*model.User, string, error) {
	key := model.NewUUID()
	u, created, err := s.store.RegisterTelegramUser(ctx, telegramID, model.NewToken(), key)
	if err != nil {
		return nil, "", err
	}
	if !created {
		return u, "", nil
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "user_registered",
		slog.Int64("user_id", u.ID),
		slog.Int64("telegram_id", telegramID))
	return u, key, nil
}

func (s *Service) HandlePayment(ctx context.Context, paymentID string, userID int64, planID int64, amountRub int64) error {
	_, err := s.RecordPayment(ctx, paymentID, userID, planID, amountRub)
	return err
}

func (s *Service) RecordPayment(ctx context.Context, paymentID string, userID int64, planID int64, amountRub int64) (store.SettleResult, error) {
	if _, err := s.store.GetPlan(ctx, planID); err != nil {
		return store.SettleResult{}, fmt.Errorf("plan %d: %w", planID, err)
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return store.SettleResult{}, err
	}
	res, err := s.store.RecordManualPayment(ctx, &model.Payment{PaymentID: paymentID, UserID: userID, PlanID: planID, AmountRub: amountRub}, time.Now().UTC())
	if errors.Is(err, store.ErrConflict) {
		return store.SettleResult{}, &ConflictError{Field: "payment_id", Message: "payment_id already belongs to another payment"}
	}
	if err != nil {
		return store.SettleResult{}, err
	}
	if !res.Applied {
		s.log.LogAttrs(ctx, slog.LevelInfo, "payment_duplicate_ignored",
			slog.String("payment_id", paymentID),
			slog.String("status", res.Payment.Status))
		return res, nil
	}
	s.logApplied(ctx, res)
	return res, nil
}

func (s *Service) logApplied(ctx context.Context, res store.SettleResult) {
	mode := "queued"
	if res.Period.StartsNow {
		mode = "now"
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "payment_applied",
		slog.String("payment_id", res.Payment.PaymentID),
		slog.Int64("user_id", res.Payment.UserID),
		slog.Int64("plan_id", res.Payment.PlanID),
		slog.String("source", res.Payment.Source),
		slog.String("mode", mode))
}

func (s *Service) RotateUser(ctx context.Context, userID int64) (string, error) {
	token := model.NewToken()
	if _, err := s.store.RotateUserCredentials(ctx, userID, token); err != nil {
		return "", err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "user_link_rotated", slog.Int64("user_id", userID))
	return token, nil
}

type UserStatus struct {
	User         *model.User         `json:"user"`
	Subscription *model.Subscription `json:"subscription"`
	Plan         *model.Plan         `json:"plan"`
	Devices      int                 `json:"devices"`
	Trial        *model.Plan         `json:"trial"`
}

func (s *Service) GetUserStatus(ctx context.Context, telegramID int64) (*UserStatus, error) {
	u, err := s.store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	st := &UserStatus{User: u}
	sub, err := s.store.GetActiveSubscription(ctx, u.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return nil, err
	default:
		plan, err := s.store.GetPlan(ctx, sub.PlanID)
		if err != nil {
			return nil, err
		}
		st.Subscription, st.Plan = sub, plan
	}
	if st.Devices, err = s.store.CountDevices(ctx, u.ID); err != nil {
		return nil, err
	}
	if st.Trial, err = s.store.TrialPlanFor(ctx, u.ID); err != nil {
		return nil, err
	}
	return st, nil
}
