package store

import (
	"context"
)

func (s *Store) AddDevice(ctx context.Context, userID int64, fingerprint string, userAgent string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO devices (user_id, fingerprint, user_agent) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, fingerprint) DO UPDATE SET last_seen = now(), user_agent = EXCLUDED.user_agent`,
		userID, fingerprint, userAgent)
	return err
}

func (s *Store) CountDevices(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE user_id = $1`, userID).Scan(&count)
	return count, err
}

func (s *Store) ResetDevices(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM devices WHERE user_id = $1`, userID)
	return err
}

func (s *Store) HasDevice(ctx context.Context, userID int64, fingerprint string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM devices WHERE user_id = $1 AND fingerprint = $2)`,
		userID, fingerprint).Scan(&exists)
	return exists, err
}
