package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	if u.Status == "" {
		u.Status = model.UserActive
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO users (telegram_id, email, sub_token, status)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		u.TelegramID, u.Email, u.SubToken, u.Status,
	).Scan(&u.ID, &u.CreatedAt)
}

const userColumns = `id, telegram_id, email, sub_token, status, banned_at, ban_reason, created_at`

func (s *Store) scanUser(row pgxRow) (*model.User, error) {
	u := &model.User{}
	err := row.Scan(&u.ID, &u.TelegramID, &u.Email, &u.SubToken, &u.Status, &u.BannedAt, &u.BanReason, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByTelegramID(ctx context.Context, telegramID int64) (*model.User, error) {
	return s.scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE telegram_id = $1`, telegramID))
}

func (s *Store) GetUserBySubToken(ctx context.Context, token string) (*model.User, error) {
	return s.scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE sub_token = $1`, token))
}

func (s *Store) AddUserKey(ctx context.Context, userID int64, uuid string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_keys (user_id, uuid) VALUES ($1, $2)`,
		userID, uuid)
	return err
}

func (s *Store) GetUserKeys(ctx context.Context, userID int64) ([]model.UserKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, uuid, created_at FROM user_keys WHERE user_id = $1 AND revoked_at IS NULL ORDER BY id`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []model.UserKey
	for rows.Next() {
		var k model.UserKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.UUID, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *Store) RotateUserCredentials(ctx context.Context, userID int64, subToken string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE users SET sub_token = $2 WHERE id = $1`, userID, subToken)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE user_keys SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
		return "", err
	}
	uuid := model.NewUUID()
	if _, err := tx.Exec(ctx, `INSERT INTO user_keys (user_id, uuid) VALUES ($1, $2)`, userID, uuid); err != nil {
		return "", err
	}
	return uuid, tx.Commit(ctx)
}

func (s *Store) RegisterTelegramUser(ctx context.Context, telegramID int64, subToken, keyUUID string) (*model.User, bool, error) {
	if u, err := s.GetUserByTelegramID(ctx, telegramID); err == nil {
		return u, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	u, err := s.scanUser(tx.QueryRow(ctx,
		`INSERT INTO users (telegram_id, email, sub_token, status) VALUES ($1, '', $2, 'active')
		 ON CONFLICT (telegram_id) DO NOTHING
		 RETURNING `+userColumns, telegramID, subToken))
	if errors.Is(err, ErrNotFound) {
		existing, err := s.scanUser(tx.QueryRow(ctx,
			`SELECT `+userColumns+` FROM users WHERE telegram_id = $1`, telegramID))
		return existing, false, err
	}
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_keys (user_id, uuid) VALUES ($1, $2)`, u.ID, keyUUID); err != nil {
		return nil, false, err
	}
	return u, true, tx.Commit(ctx)
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	return s.scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}
