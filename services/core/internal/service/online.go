package service

import (
	"context"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	maxIPHistoryPage       = 100
	defaultIPRetentionDays = 30
)

func (s *Service) UserPresence(ctx context.Context, userID int64) (store.Presence, error) {
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return store.Presence{}, err
	}
	return s.store.UserPresence(ctx, userID)
}

func (s *Service) UserIPHistory(ctx context.Context, userID int64, limit, offset int) ([]store.IPHistoryRow, int, error) {
	if limit < 1 || limit > maxIPHistoryPage {
		return nil, 0, fieldErr("limit", "must be 1-100")
	}
	if offset < 0 {
		return nil, 0, fieldErr("offset", "must not be negative")
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return nil, 0, err
	}
	return s.store.UserIPHistory(ctx, userID, limit, offset)
}

func (s *Service) NodeOnline(ctx context.Context, nodeID int64) ([]store.NodeOnlineUser, error) {
	if _, err := s.store.GetNode(ctx, nodeID); err != nil {
		return nil, err
	}
	return s.store.NodeOnline(ctx, nodeID)
}
