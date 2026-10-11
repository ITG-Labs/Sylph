package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Service) RecordNodeDeltas(ctx context.Context, nodeCN string, batchID string, deltas []model.TrafficDelta) (bool, error) {
	node, err := s.store.GetNodeByCN(ctx, nodeCN)
	if err != nil {
		return false, err
	}
	duplicate, err := s.store.RecordDeltaBatch(ctx, node.ID, batchID, deltas)
	if err != nil {
		return false, err
	}
	if duplicate {
		s.log.LogAttrs(ctx, slog.LevelInfo, "delta_batch_duplicate",
			slog.String("node", nodeCN),
			slog.String("batch_id", batchID))
	}
	return duplicate, s.applyTrafficLimits(ctx, deltas)
}

func (s *Service) applyTrafficLimits(ctx context.Context, deltas []model.TrafficDelta) error {
	keyOwners := make(map[string]int64)
	for _, d := range deltas {
		keyOwners[d.KeyUUID] = 0
	}
	uuids := make([]string, 0, len(keyOwners))
	for uuid := range keyOwners {
		uuids = append(uuids, uuid)
	}
	users, err := s.store.ListUsersByKeys(ctx, uuids)
	if err != nil {
		return err
	}
	for _, u := range users {
		sub, err := s.store.GetActiveSubscription(ctx, u.ID)
		if err != nil {
			if err == store.ErrNotFound {
				continue
			}
			return err
		}
		used, err := s.store.SumTrafficSince(ctx, u.ID, sub.StartsAt)
		if err != nil {
			return err
		}
		if err := s.store.SetTrafficUsed(ctx, sub.ID, used); err != nil {
			return err
		}
		plan, err := s.store.GetPlan(ctx, sub.PlanID)
		if err != nil {
			return err
		}
		if plan.TrafficLimitBytes != nil && used > *plan.TrafficLimitBytes {
			if err := s.store.SetUserStatus(ctx, u.ID, "expired_traffic"); err != nil {
				return err
			}
			s.log.LogAttrs(ctx, slog.LevelInfo, "user_traffic_expired",
				slog.Int64("user_id", u.ID),
				slog.Int64("used", used),
				slog.Int64("limit", *plan.TrafficLimitBytes))
		}
	}
	return nil
}

func (s *Service) CheckAndExpire(ctx context.Context, now time.Time) (int64, error) {
	n, err := s.store.ExpireOverdue(ctx, now)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		s.log.LogAttrs(ctx, slog.LevelInfo, "subscriptions_expired", slog.Int64("count", n))
	}
	return n, nil
}

type TrafficDeltaInput struct {
	KeyUUID   string
	BytesUp   int64
	BytesDown int64
}

func (s *Service) RecordNodeDeltasInput(ctx context.Context, nodeCN string, batchID string, deltas []TrafficDeltaInput) (bool, error) {
	models := make([]model.TrafficDelta, 0, len(deltas))
	for _, d := range deltas {
		models = append(models, model.TrafficDelta{KeyUUID: d.KeyUUID, BytesUp: d.BytesUp, BytesDown: d.BytesDown})
	}
	return s.RecordNodeDeltas(ctx, nodeCN, batchID, models)
}

func (s *Service) NodeHeartbeat(ctx context.Context, nodeCN string, hb model.NodeHeartbeat) error {
	node, err := s.store.GetNodeByCN(ctx, nodeCN)
	if err != nil {
		return err
	}
	return s.store.TouchNodeHeartbeat(ctx, node.ID, hb)
}
