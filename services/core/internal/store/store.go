package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

var ErrConflict = errors.New("conflict")

var ErrInvalidReference = errors.New("invalid reference")

type Store struct {
	pool *pgxpool.Pool
}

type pgxRow interface {
	Scan(dest ...any) error
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func mapWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrConflict
		case "23503":
			return ErrInvalidReference
		}
	}
	return err
}
