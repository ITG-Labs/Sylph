package service

import (
	"log/slog"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

type Service struct {
	store *store.Store
	log   *slog.Logger
}

func NewService(st *store.Store, log *slog.Logger) *Service {
	return &Service{store: st, log: log}
}
