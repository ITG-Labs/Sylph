package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type DeviceInfo struct {
	HWID      string
	UserAgent string
	OS        string
	Model     string
}

func (s *Store) RegisterDevice(ctx context.Context, userID int64, d DeviceInfo, limit *int) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	if limit != nil {
		var known bool
		var count int
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM devices WHERE user_id = $1 AND fingerprint = $2),
			        (SELECT count(*) FROM devices WHERE user_id = $1)`,
			userID, d.HWID).Scan(&known, &count); err != nil {
			return false, err
		}
		if !known && count >= *limit {
			return false, nil
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO devices (user_id, fingerprint, user_agent, device_os, device_model) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id, fingerprint) DO UPDATE SET last_seen = now(), user_agent = EXCLUDED.user_agent,
		 device_os = EXCLUDED.device_os, device_model = EXCLUDED.device_model`,
		userID, d.HWID, d.UserAgent, d.OS, d.Model); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Store) CountDevices(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE user_id = $1`, userID).Scan(&count)
	return count, err
}

func (s *Store) ResetDevices(ctx context.Context, userID int64) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM devices WHERE user_id = $1`, userID)
	return err
}
