package store

import (
	"context"
	"time"
)

type Settings struct {
	SubsTitle      string    `json:"subs_title"`
	SubsSupportURL string    `json:"subs_support_url"`
	GeoGeneration  int64     `json:"geo_generation"`
	NotifyTraffic  bool      `json:"notify_traffic"`
	NotifyExpiry   bool      `json:"notify_expiry"`
	NotifyTrial    bool      `json:"notify_trial"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SettingsPatch struct {
	SubsTitle      *string
	SubsSupportURL *string
	NotifyTraffic  *bool
	NotifyExpiry   *bool
	NotifyTrial    *bool
}

const settingsColumns = `subs_title, subs_support_url, geo_generation, notify_traffic, notify_expiry, notify_trial, updated_at`

func scanSettings(row pgxRow) (Settings, error) {
	var st Settings
	err := row.Scan(&st.SubsTitle, &st.SubsSupportURL, &st.GeoGeneration, &st.NotifyTraffic, &st.NotifyExpiry, &st.NotifyTrial, &st.UpdatedAt)
	return st, err
}

func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	return scanSettings(s.pool.QueryRow(ctx, `SELECT `+settingsColumns+` FROM settings`))
}

func (s *Store) UpdateSettings(ctx context.Context, p SettingsPatch) (Settings, error) {
	return scanSettings(s.pool.QueryRow(ctx,
		`UPDATE settings SET subs_title = COALESCE($1, subs_title), subs_support_url = COALESCE($2, subs_support_url),
		        notify_traffic = COALESCE($3, notify_traffic), notify_expiry = COALESCE($4, notify_expiry),
		        notify_trial = COALESCE($5, notify_trial), updated_at = now()
		 RETURNING `+settingsColumns, p.SubsTitle, p.SubsSupportURL, p.NotifyTraffic, p.NotifyExpiry, p.NotifyTrial))
}
