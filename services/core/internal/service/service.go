package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

type Service struct {
	store       *store.Store
	log         *slog.Logger
	pay         PaymentProvider
	returnURL   string
	createLocks [64]sync.Mutex
	pollBudget  time.Duration
	nodeOffline time.Duration
	nodeStale   time.Duration
	botUsername string
}

func (s *Service) SetBotUsername(name string) {
	s.botUsername = name
}

func (s *Service) withLinks(invites ...*model.Invite) {
	if s.botUsername == "" {
		return
	}
	for _, inv := range invites {
		link := "https://t.me/" + s.botUsername + "?start=" + inv.Code
		inv.Link = &link
	}
}

func NewService(st *store.Store, log *slog.Logger) *Service {
	return &Service{store: st, log: log, pollBudget: 20 * time.Second, nodeOffline: DefaultNodeOffline, nodeStale: DefaultNodeSyncStale}
}

func (s *Service) UpsertNode(ctx context.Context, n *model.Node) error {
	return s.store.UpsertNode(ctx, n)
}

func (s *Service) GetNodeByCN(ctx context.Context, cn string) (*model.Node, error) {
	return s.store.GetNodeByCN(ctx, cn)
}

func (s *Service) CreatePlan(ctx context.Context, p *model.Plan) error {
	return s.store.CreatePlan(ctx, p)
}

func (s *Service) RenewSubscription(ctx context.Context, userID int64, planID int64) (int64, error) {
	res, err := s.store.ApplyPayment(ctx, userID, planID, time.Now().UTC())
	return res.PeriodID, err
}

func (s *Service) GetUserByTelegramID(ctx context.Context, tg int64) (*model.User, error) {
	return s.store.GetUserByTelegramID(ctx, tg)
}

func (s *Service) GetUserFullByToken(ctx context.Context, token string) (*model.User, []model.UserKey, *model.Subscription, *model.Plan, error) {
	u, err := s.store.GetUserBySubToken(ctx, token)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	keys, err := s.store.GetUserKeys(ctx, u.ID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	sub, err := s.store.GetActiveSubscription(ctx, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		return u, keys, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, nil, err
	}
	plan, err := s.store.GetPlan(ctx, sub.PlanID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return u, keys, sub, plan, nil
}

func (s *Service) ListPlansOnSale(ctx context.Context) ([]model.Plan, error) {
	return s.store.ListPlansOnSale(ctx)
}

func (s *Service) ListUserDevices(ctx context.Context, userID int64) ([]model.Device, error) {
	if _, err := s.activeUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.store.ListDevices(ctx, userID)
}

func (s *Service) RemoveUserDevice(ctx context.Context, userID, deviceID int64) error {
	if _, err := s.activeUser(ctx, userID); err != nil {
		return err
	}
	if err := s.store.RemoveDevice(ctx, userID, deviceID); err != nil {
		return err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "device_removed", slog.Int64("user_id", userID), slog.Int64("device_id", deviceID))
	return nil
}

func (s *Service) ResetUserDevices(ctx context.Context, userID int64) error {
	if _, err := s.activeUser(ctx, userID); err != nil {
		return err
	}
	return s.store.ResetDevices(ctx, userID)
}
