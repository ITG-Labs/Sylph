package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type TelegramProfile struct {
	Username  *string
	FirstName string
	LastName  string
}

type TelegramInfo struct {
	Username  *string   `json:"username"`
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updated_at"`
}

const telegramNameSQL = `NULLIF(btrim(tp.first_name || ' ' || tp.last_name), '')`

func profileSearch(p TelegramProfile) string {
	nick := ""
	if p.Username != nil {
		nick = *p.Username
	}
	return strings.ToLower(nick + "\n" + p.FirstName + " " + p.LastName)
}

func (s *Store) SaveTelegramProfile(ctx context.Context, userID int64, p TelegramProfile) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO telegram_profiles (user_id, username, first_name, last_name, search)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id) DO UPDATE SET
		   username = EXCLUDED.username, first_name = EXCLUDED.first_name, last_name = EXCLUDED.last_name,
		   search = EXCLUDED.search, updated_at = now()
		 WHERE (telegram_profiles.username, telegram_profiles.first_name, telegram_profiles.last_name)
		   IS DISTINCT FROM (EXCLUDED.username, EXCLUDED.first_name, EXCLUDED.last_name)`,
		userID, p.Username, p.FirstName, p.LastName, profileSearch(p))
	return mapWriteErr(err)
}

func (s *Store) telegramInfo(ctx context.Context, userID int64) (*TelegramInfo, error) {
	var info TelegramInfo
	var name *string
	err := s.pool.QueryRow(ctx,
		`SELECT tp.username, `+telegramNameSQL+`, tp.updated_at FROM telegram_profiles tp WHERE tp.user_id = $1`,
		userID).Scan(&info.Username, &name, &info.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if name != nil {
		info.Name = *name
	}
	return &info, nil
}
