package service

import (
	"context"
	"errors"
	"net/url"
	"unicode"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	maxSupportURLBytes = 512
	defaultSubsTitle   = "Sylph"
)

type SettingsPatch struct {
	SubsTitle      *string             `json:"subs_title"`
	SubsSupportURL *string             `json:"subs_support_url"`
	NotifyTraffic  *bool               `json:"notify_traffic"`
	NotifyExpiry   *bool               `json:"notify_expiry"`
	NotifyTrial    *bool               `json:"notify_trial"`
	TermsEnabled   *bool               `json:"terms_enabled"`
	TermsText      *string             `json:"terms_text"`
	TermsURL       *string             `json:"terms_url"`
	ChannelEnabled *bool               `json:"channel_enabled"`
	ChannelChat    *string             `json:"channel_chat"`
	ChannelURL     *string             `json:"channel_url"`
	ChannelText    *string             `json:"channel_text"`
	HelpEnabled    *bool               `json:"help_enabled"`
	HelpText       *string             `json:"help_text"`
	HelpButtons    *[]store.HelpButton `json:"help_buttons"`
}

func validSupportURL(v string) error {
	const field = "subs_support_url"
	if len(v) > maxSupportURLBytes {
		return fieldErr(field, "must be at most 512 bytes")
	}
	for _, r := range v {
		if r > unicode.MaxASCII || unicode.IsControl(r) || unicode.IsSpace(r) {
			return fieldErr(field, "must be ASCII without spaces or control characters (use punycode for non-Latin domains)")
		}
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fieldErr(field, "must be an http(s) URL or empty")
	}
	return nil
}

func (s *Service) GetSettings(ctx context.Context) (store.Settings, error) {
	return s.store.GetSettings(ctx)
}

func (s *Service) UpdateSettings(ctx context.Context, p SettingsPatch) (store.Settings, error) {
	if p.SubsTitle != nil {
		if err := validName("subs_title", *p.SubsTitle); err != nil {
			return store.Settings{}, err
		}
	}
	if p.SubsSupportURL != nil && *p.SubsSupportURL != "" {
		if err := validSupportURL(*p.SubsSupportURL); err != nil {
			return store.Settings{}, err
		}
	}
	if err := s.checkBotPatch(ctx, &p); err != nil {
		return store.Settings{}, err
	}
	st, err := s.store.UpdateSettings(ctx, store.SettingsPatch{
		SubsTitle: p.SubsTitle, SubsSupportURL: p.SubsSupportURL,
		NotifyTraffic: p.NotifyTraffic, NotifyExpiry: p.NotifyExpiry, NotifyTrial: p.NotifyTrial,
		TermsEnabled: p.TermsEnabled, TermsText: p.TermsText, TermsURL: p.TermsURL,
		ChannelEnabled: p.ChannelEnabled, ChannelChat: p.ChannelChat, ChannelURL: p.ChannelURL, ChannelText: p.ChannelText,
		HelpEnabled: p.HelpEnabled, HelpText: p.HelpText, HelpButtons: p.HelpButtons,
	})
	if errors.Is(err, store.ErrConflict) {
		return store.Settings{}, &ConflictError{Field: "settings", Message: "changed concurrently, reload and try again"}
	}
	return st, err
}
