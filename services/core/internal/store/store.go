package store

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

var ErrConflict = errors.New("conflict")

var ErrInvalidReference = errors.New("invalid reference")

var ErrTrialOnSale = fmt.Errorf("%w: another trial plan is on sale", ErrConflict)

var ErrGroupNotEmpty = errors.New("group is not empty")

type Store struct {
	pool    *pgxpool.Pool
	statsTZ string
}

type pgxRow interface {
	Scan(dest ...any) error
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, statsTZ: DefaultStatsTimezone}
}

func mapWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if pgErr.ConstraintName == "plans_one_trial_on_sale" {
				return ErrTrialOnSale
			}
			return ErrConflict
		case "23503":
			return ErrInvalidReference
		}
	}
	return err
}
