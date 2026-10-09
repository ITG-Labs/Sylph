package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	pool *pgxpool.Pool
}

type pgxRow interface {
	Scan(dest ...any) error
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
