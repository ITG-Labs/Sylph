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
	u := &model.User{TelegramID: &telegramID, SubToken: model.NewToken(), Status: "active"}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return nil, "", err
	}
	key := model.NewUUID()
	if err := s.store.AddUserKey(ctx, u.ID, key); err != nil {
		return nil, "", err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "user_registered",
		slog.Int64("user_id", u.ID),
		slog.Int64("telegram_id", telegramID))
	return u, key, nil
}

func (s *Service) HandlePayment(ctx context.Context, paymentID string, userID int64, planID int64, amountRub int64) error {
	if _, err := s.store.GetPlan(ctx, planID); err != nil {
		return fmt.Errorf("plan %d: %w", planID, err)
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return err
	}
	existing, err := s.store.GetPayment(ctx, paymentID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if existing != nil && existing.Status == "succeeded" {
		s.log.LogAttrs(ctx, slog.LevelInfo, "payment_duplicate_ignored",
			slog.String("payment_id", paymentID))
		return nil
	}
	if existing == nil {
		if err := s.store.CreatePayment(ctx, &model.Payment{PaymentID: paymentID, UserID: userID, PlanID: planID, AmountRub: amountRub, Status: "pending"}); err != nil {
			return err
		}
	} else {
		if err := s.store.SetPaymentStatus(ctx, paymentID, "pending"); err != nil {
			return err
		}
	}
	if _, err := s.store.RenewSubscription(ctx, userID, planID, time.Now().UTC()); err != nil {
		return err
	}
	if err := s.store.SetPaymentStatus(ctx, paymentID, "succeeded"); err != nil {
		return err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "payment_applied",
		slog.String("payment_id", paymentID),
		slog.Int64("user_id", userID),
		slog.Int64("plan_id", planID))
	return nil
}

func (s *Service) RotateUserKey(ctx context.Context, userID int64) (string, error) {
	keys, err := s.store.RotateUserKeys(ctx, userID)
	if err != nil {
		return "", err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "user_key_rotated", slog.Int64("user_id", userID))
	return keys[0].UUID, nil
}

func (s *Service) GetUserSubscriptionInfo(ctx context.Context, telegramID int64) (*model.Subscription, *model.Plan, error) {
	u, err := s.store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, nil, err
	}
	sub, err := s.store.GetActiveSubscription(ctx, u.ID)
	if err != nil {
		return nil, nil, err
	}
	plan, err := s.store.GetPlan(ctx, sub.PlanID)
	if err != nil {
		return nil, nil, err
	}
	return sub, plan, nil
}
