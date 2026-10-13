package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxInviteUses = 100000

func (s *Service) UserPlans(ctx context.Context, userID int64) ([]model.Plan, error) {
	if _, err := s.activeUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.store.VisiblePlans(ctx, userID)
}

func (s *Service) planAvailable(ctx context.Context, plan *model.Plan, userID int64) (bool, error) {
	if !plan.OnSale {
		return false, nil
	}
	if plan.Visibility == model.VisibilityPublic {
		return true, nil
	}
	return s.store.HasPlanAccess(ctx, plan.ID, userID)
}

func (s *Service) RedeemInvite(ctx context.Context, userID int64, code string) (*model.Plan, bool, error) {
	if !model.ValidInviteCode(code) {
		return nil, false, fieldErr("code", "invalid invite code")
	}
	if _, err := s.activeUser(ctx, userID); err != nil {
		return nil, false, err
	}
	res, err := s.store.RedeemInvite(ctx, code, userID, time.Now().UTC())
	if err != nil {
		reason := "error"
		switch {
		case errors.Is(err, store.ErrNotFound):
			reason = "unknown"
		case errors.Is(err, store.ErrInviteRevoked):
			reason = "revoked"
		case errors.Is(err, store.ErrInviteExpired):
			reason = "expired"
		case errors.Is(err, store.ErrInviteUsedUp):
			reason = "used_up"
		case errors.Is(err, store.ErrAccessRevoked):
			reason = "access_revoked"
		}
		s.log.LogAttrs(ctx, slog.LevelInfo, "invite_rejected", slog.Int64("user_id", userID), slog.String("reason", reason))
		return nil, false, err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "invite_redeemed",
		slog.Int64("invite_id", res.InviteID),
		slog.Int64("user_id", userID),
		slog.Int64("plan_id", res.PlanID),
		slog.Bool("already", res.Already))
	plan, err := s.store.GetPlan(ctx, res.PlanID)
	if err != nil {
		return nil, false, err
	}
	return plan, res.Already, nil
}

func (s *Service) CreateInvite(ctx context.Context, planID int64, maxUses *int, expiresAt *time.Time) (*model.Invite, error) {
	if maxUses != nil && (*maxUses < 1 || *maxUses > maxInviteUses) {
		return nil, fieldErr("max_uses", "must be 1-100000 or null for unlimited")
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return nil, fieldErr("expires_at", "must be in the future or null for no expiry")
	}
	if _, err := s.store.GetPlan(ctx, planID); err != nil {
		return nil, err
	}
	inv := &model.Invite{Code: model.NewInviteCode(), PlanID: planID, MaxUses: maxUses, ExpiresAt: expiresAt}
	if err := s.store.CreateInvite(ctx, inv); err != nil {
		return nil, err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "invite_created", slog.Int64("invite_id", inv.ID), slog.Int64("plan_id", planID))
	s.withLinks(inv)
	return inv, nil
}

func (s *Service) ListInvites(ctx context.Context, planID int64) ([]model.Invite, error) {
	if _, err := s.store.GetPlan(ctx, planID); err != nil {
		return nil, err
	}
	list, err := s.store.ListInvites(ctx, planID)
	for i := range list {
		s.withLinks(&list[i])
	}
	return list, err
}

func (s *Service) RevokeInvite(ctx context.Context, id int64) (*model.Invite, error) {
	inv, err := s.store.RevokeInvite(ctx, id, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "invite_revoked", slog.Int64("invite_id", id), slog.Int64("plan_id", inv.PlanID))
	s.withLinks(inv)
	return inv, nil
}

func (s *Service) ListPlanAccess(ctx context.Context, planID int64) ([]model.PlanAccess, error) {
	if _, err := s.store.GetPlan(ctx, planID); err != nil {
		return nil, err
	}
	return s.store.ListPlanAccess(ctx, planID)
}

func (s *Service) GrantPlanAccess(ctx context.Context, planID, telegramID int64) error {
	if telegramID <= 0 {
		return fieldErr("telegram_id", "must be positive")
	}
	if _, err := s.store.GetPlan(ctx, planID); err != nil {
		return err
	}
	u, _, err := s.RegisterUser(ctx, telegramID)
	if err != nil {
		return err
	}
	if u.Status == model.UserBanned {
		return &ConflictError{Field: "telegram_id", Message: "user is banned"}
	}
	if err := s.store.GrantPlanAccess(ctx, planID, u.ID, store.AccessAdmin, nil); err != nil {
		return err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "plan_access_granted", slog.Int64("plan_id", planID), slog.Int64("user_id", u.ID))
	return nil
}

func (s *Service) RevokePlanAccess(ctx context.Context, planID, telegramID int64) error {
	u, err := s.store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return err
	}
	if err := s.store.RevokePlanAccess(ctx, planID, u.ID); err != nil {
		return err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "plan_access_revoked", slog.Int64("plan_id", planID), slog.Int64("user_id", u.ID))
	return nil
}

func (s *Service) UserStats(ctx context.Context, userID int64) (store.UserStats, error) {
	if _, err := s.activeUser(ctx, userID); err != nil {
		return store.UserStats{}, err
	}
	return s.store.UserStats(ctx, userID)
}
