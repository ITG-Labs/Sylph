package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	minRetentionDays = 1
	maxRetentionDays = 3650
	cleanupChunk     = 10000
)

func (s *Service) RecordNodeDeltas(ctx context.Context, nodeCN string, batchID string, deltas []model.TrafficDelta) (bool, error) {
	node, err := s.store.GetNodeByCN(ctx, nodeCN)
	if err != nil {
		return false, err
	}
	res, err := s.store.RecordDeltaBatch(ctx, node.ID, batchID, deltas)
	if err != nil {
		return false, err
	}
	if res.Duplicate {
		s.log.LogAttrs(ctx, slog.LevelInfo, "delta_batch_duplicate",
			slog.String("node", nodeCN),
			slog.String("batch_id", batchID))
	}
	for _, e := range res.Exhausted {
		s.log.LogAttrs(ctx, slog.LevelInfo, "traffic_period_exhausted",
			slog.Int64("user_id", e.UserID),
			slog.Int64("used", e.Used),
			slog.Int64("limit", e.Limit))
	}
	return res.Duplicate, nil
}

func (s *Service) CleanupHistory(ctx context.Context, olderThanDays int, statsOlderThanDays *int, now time.Time) (store.CleanupResult, error) {
	if olderThanDays < minRetentionDays || olderThanDays > maxRetentionDays {
		return store.CleanupResult{}, fieldErr("older_than_days", "must be 1-3650")
	}
	if statsOlderThanDays != nil && (*statsOlderThanDays < minRetentionDays || *statsOlderThanDays > maxRetentionDays) {
		return store.CleanupResult{}, fieldErr("stats_older_than_days", "must be 1-3650")
	}
	res, err := s.store.CleanupHistory(ctx, now.AddDate(0, 0, -olderThanDays), cleanupChunk)
	if err != nil {
		return res, err
	}
	if statsOlderThanDays != nil {
		if res.Daily, err = s.store.CleanupDailyStats(ctx, *statsOlderThanDays); err != nil {
			return res, err
		}
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "maintenance_cleanup",
		slog.Int("older_than_days", olderThanDays),
		slog.Int64("deltas", res.Deltas),
		slog.Int64("batches", res.Batches),
		slog.Int64("keys", res.Keys),
		slog.Int64("daily", res.Daily),
		slog.Int64("notifications", res.Notifications))
	return res, nil
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
