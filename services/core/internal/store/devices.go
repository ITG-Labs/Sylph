package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

type DeviceInfo struct {
	HWID      string
	UserAgent string
	OS        string
	Model     string
}

type DeviceVerdict int

const (
	DeviceAllowed DeviceVerdict = iota
	DeviceOverLimit
	DeviceTrialTaken
)

func (s *Store) RegisterDevice(ctx context.Context, userID int64, d DeviceInfo, limit *int) (bool, error) {
	v, err := s.registerDevice(ctx, userID, d, limit, false)
	return v == DeviceAllowed, err
}

func (s *Store) RegisterTrialDevice(ctx context.Context, userID int64, d DeviceInfo, limit *int) (DeviceVerdict, error) {
	return s.registerDevice(ctx, userID, d, limit, true)
}

func hwidHash(hwid string) string {
	sum := sha256.Sum256([]byte(hwid))
	return hex.EncodeToString(sum[:])
}

func (s *Store) registerDevice(ctx context.Context, userID int64, d DeviceInfo, limit *int, trial bool) (DeviceVerdict, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if limit != nil {
		var known bool
		var count int
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM devices WHERE user_id = $1 AND fingerprint = $2),
			        (SELECT count(*) FROM devices WHERE user_id = $1)`,
			userID, d.HWID).Scan(&known, &count); err != nil {
			return 0, err
		}
		if !known && count >= *limit {
			return DeviceOverLimit, nil
		}
	}
	if trial {
		hash := hwidHash(d.HWID)
		if _, err := tx.Exec(ctx, `INSERT INTO trial_devices (hwid_hash, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, hash, userID); err != nil {
			return 0, err
		}
		var owner int64
		if err := tx.QueryRow(ctx, `SELECT user_id FROM trial_devices WHERE hwid_hash = $1`, hash).Scan(&owner); err != nil {
			return 0, err
		}
		if owner != userID {
			return DeviceTrialTaken, nil
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO devices (user_id, fingerprint, user_agent, device_os, device_model) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id, fingerprint) DO UPDATE SET last_seen = now(), user_agent = EXCLUDED.user_agent,
		 device_os = EXCLUDED.device_os, device_model = EXCLUDED.device_model`,
		userID, d.HWID, d.UserAgent, d.OS, d.Model); err != nil {
		return 0, err
	}
	return DeviceAllowed, tx.Commit(ctx)
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

func (s *Store) ListDevices(ctx context.Context, userID int64) ([]model.Device, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, user_agent, device_os, device_model, first_seen, last_seen
		 FROM devices WHERE user_id = $1 ORDER BY first_seen, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Device{}
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.UserID, &d.UserAgent, &d.OS, &d.Model, &d.FirstSeen, &d.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) RemoveDevice(ctx context.Context, userID, deviceID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM devices WHERE id = $1 AND user_id = $2`, deviceID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
