package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type HelpButton struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type Settings struct {
	SubsTitle      string       `json:"subs_title"`
	SubsSupportURL string       `json:"subs_support_url"`
	GeoGeneration  int64        `json:"geo_generation"`
	NotifyTraffic  bool         `json:"notify_traffic"`
	NotifyExpiry   bool         `json:"notify_expiry"`
	NotifyTrial    bool         `json:"notify_trial"`
	TermsEnabled   bool         `json:"terms_enabled"`
	TermsText      string       `json:"terms_text"`
	TermsURL       string       `json:"terms_url"`
	TermsVersion   int          `json:"terms_version"`
	ChannelEnabled bool         `json:"channel_enabled"`
	ChannelChat    string       `json:"channel_chat"`
	ChannelURL     string       `json:"channel_url"`
	ChannelText    string       `json:"channel_text"`
	HelpEnabled    bool         `json:"help_enabled"`
	HelpText       string       `json:"help_text"`
	HelpButtons    []HelpButton `json:"help_buttons"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

type SettingsPatch struct {
	SubsTitle      *string
	SubsSupportURL *string
	NotifyTraffic  *bool
	NotifyExpiry   *bool
	NotifyTrial    *bool
	TermsEnabled   *bool
	TermsText      *string
	TermsURL       *string
	ChannelEnabled *bool
	ChannelChat    *string
	ChannelURL     *string
	ChannelText    *string
	HelpEnabled    *bool
	HelpText       *string
	HelpButtons    *[]HelpButton
}

const settingsColumns = `subs_title, subs_support_url, geo_generation, notify_traffic, notify_expiry, notify_trial,
	terms_enabled, terms_text, terms_url, terms_version, channel_enabled, channel_chat, channel_url, channel_text,
	help_enabled, help_text, help_buttons, updated_at`

func scanSettings(row pgxRow) (Settings, error) {
	var st Settings
	err := row.Scan(&st.SubsTitle, &st.SubsSupportURL, &st.GeoGeneration, &st.NotifyTraffic, &st.NotifyExpiry, &st.NotifyTrial,
		&st.TermsEnabled, &st.TermsText, &st.TermsURL, &st.TermsVersion, &st.ChannelEnabled, &st.ChannelChat, &st.ChannelURL, &st.ChannelText,
		&st.HelpEnabled, &st.HelpText, &st.HelpButtons, &st.UpdatedAt)
	if st.HelpButtons == nil {
		st.HelpButtons = []HelpButton{}
	}
	return st, err
}

func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	return scanSettings(s.pool.QueryRow(ctx, `SELECT `+settingsColumns+` FROM settings`))
}

func (s *Store) UpdateSettings(ctx context.Context, p SettingsPatch) (Settings, error) {
	st, err := scanSettings(s.pool.QueryRow(ctx,
		`UPDATE settings SET subs_title = COALESCE($1, subs_title), subs_support_url = COALESCE($2, subs_support_url),
		        notify_traffic = COALESCE($3, notify_traffic), notify_expiry = COALESCE($4, notify_expiry),
		        notify_trial = COALESCE($5, notify_trial),
		        terms_enabled = COALESCE($6, terms_enabled), terms_text = COALESCE($7, terms_text), terms_url = COALESCE($8, terms_url),
		        channel_enabled = COALESCE($9, channel_enabled), channel_chat = COALESCE($10, channel_chat),
		        channel_url = COALESCE($11, channel_url), channel_text = COALESCE($12, channel_text),
		        help_enabled = COALESCE($13, help_enabled), help_text = COALESCE($14, help_text),
		        help_buttons = COALESCE($15, help_buttons), updated_at = now()
		 RETURNING `+settingsColumns, p.SubsTitle, p.SubsSupportURL, p.NotifyTraffic, p.NotifyExpiry, p.NotifyTrial,
		p.TermsEnabled, p.TermsText, p.TermsURL, p.ChannelEnabled, p.ChannelChat, p.ChannelURL, p.ChannelText,
		p.HelpEnabled, p.HelpText, p.HelpButtons))
	if pgCode(err) == "23514" {
		return Settings{}, ErrConflict
	}
	return st, err
}

func (s *Store) BumpTermsVersion(ctx context.Context) (Settings, error) {
	return scanSettings(s.pool.QueryRow(ctx,
		`UPDATE settings SET terms_version = terms_version + 1, updated_at = now() RETURNING `+settingsColumns))
}

func (s *Store) AcceptTerms(ctx context.Context, userID int64, version int) error {
	var current int
	if err := s.pool.QueryRow(ctx, `SELECT terms_version FROM settings`).Scan(&current); err != nil {
		return err
	}
	if version != current {
		return ErrConflict
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO terms_acceptances (user_id, version) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, version)
	if pgCode(err) == "23503" {
		return ErrNotFound
	}
	return err
}

type TermsAcceptance struct {
	Version    int       `json:"version"`
	AcceptedAt time.Time `json:"accepted_at"`
}

func (s *Store) LatestTerms(ctx context.Context, userID int64) (*TermsAcceptance, error) {
	var t TermsAcceptance
	err := s.pool.QueryRow(ctx, `SELECT version, accepted_at FROM terms_acceptances WHERE user_id = $1 ORDER BY version DESC LIMIT 1`, userID).
		Scan(&t.Version, &t.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
