package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO users (telegram_id, email, sub_token, status)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		u.TelegramID, u.Email, u.SubToken, u.Status,
	).Scan(&u.ID, &u.CreatedAt)
}

func (s *Store) scanUser(row pgxRow) (*model.User, error) {
	u := &model.User{}
	err := row.Scan(&u.ID, &u.TelegramID, &u.Email, &u.SubToken, &u.Status, &u.CreatedAt)
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
		`SELECT id, telegram_id, email, sub_token, status, created_at FROM users WHERE telegram_id = $1`, telegramID))
}

func (s *Store) GetUserBySubToken(ctx context.Context, token string) (*model.User, error) {
	return s.scanUser(s.pool.QueryRow(ctx,
		`SELECT id, telegram_id, email, sub_token, status, created_at FROM users WHERE sub_token = $1`, token))
}

func (s *Store) AddUserKey(ctx context.Context, userID int64, uuid string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_keys (user_id, uuid) VALUES ($1, $2)`,
		userID, uuid)
	return err
}

func (s *Store) GetUserKeys(ctx context.Context, userID int64) ([]model.UserKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, uuid, created_at FROM user_keys WHERE user_id = $1 ORDER BY id`,
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

func (s *Store) RotateUserKeys(ctx context.Context, userID int64) ([]model.UserKey, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM user_keys WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	uuid := model.NewUUID()
	if _, err := tx.Exec(ctx, `INSERT INTO user_keys (user_id, uuid) VALUES ($1, $2)`, userID, uuid); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return []model.UserKey{{UserID: userID, UUID: uuid}}, nil
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	return s.scanUser(s.pool.QueryRow(ctx,
		`SELECT id, telegram_id, email, sub_token, status, created_at FROM users WHERE id = $1`, id))
}
