package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type ClientRouting struct {
	Rules     json.RawMessage
	Revision  int64
	UpdatedAt *time.Time
}

func (s *Store) GetClientRouting(ctx context.Context) (ClientRouting, error) {
	var cr ClientRouting
	err := s.pool.QueryRow(ctx, `SELECT client_rules, client_rules_revision, client_rules_updated_at FROM settings`).
		Scan(&cr.Rules, &cr.Revision, &cr.UpdatedAt)
	return cr, err
}

func (s *Store) PutClientRouting(ctx context.Context, rules json.RawMessage, revision int64) (ClientRouting, error) {
	var cr ClientRouting
	err := s.pool.QueryRow(ctx,
		`UPDATE settings SET client_rules = $1, client_rules_revision = client_rules_revision + 1, client_rules_updated_at = now()
		 WHERE client_rules_revision = $2
		 RETURNING client_rules, client_rules_revision, client_rules_updated_at`, rules, revision).
		Scan(&cr.Rules, &cr.Revision, &cr.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ClientRouting{}, ErrStaleRevision
	}
	return cr, err
}

func (s *Store) GetClientGeoTags(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT client_geo_tags FROM settings`).Scan(&raw)
	return raw, err
}

func (s *Store) PutClientGeoTags(ctx context.Context, tags json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `UPDATE settings SET client_geo_tags = $1`, tags)
	return err
}
