package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

type Service struct {
	store *store.Store
	log   *slog.Logger
}

func NewService(st *store.Store, log *slog.Logger) *Service {
	return &Service{store: st, log: log}
}

func (s *Service) UpsertNodeTest(ctx context.Context, n *model.Node) error {
	return s.store.UpsertNode(ctx, n)
}

func (s *Service) GetNodeByCNTest(ctx context.Context, cn string) (*model.Node, error) {
	return s.store.GetNodeByCN(ctx, cn)
}

func (s *Service) CreatePlanTest(ctx context.Context, p *model.Plan) error {
	return s.store.CreatePlan(ctx, p)
}

func (s *Service) RenewSubscriptionTest(ctx context.Context, userID int64, planID int64) (int64, error) {
	return s.store.RenewSubscription(ctx, userID, planID, time.Now().UTC())
}
