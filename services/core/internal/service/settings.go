package service

import (
	"context"
	"net/url"
	"unicode"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxSupportURLBytes = 512

type SettingsPatch struct {
	SubsTitle      *string `json:"subs_title"`
	SubsSupportURL *string `json:"subs_support_url"`
}

func validSupportURL(v string) error {
	const field = "subs_support_url"
	if len(v) > maxSupportURLBytes {
		return fieldErr(field, "must be at most 512 bytes")
	}
	for _, r := range v {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == ' ' || r == ' ' {
			return fieldErr(field, "must not contain spaces or control characters")
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
	return s.store.UpdateSettings(ctx, store.SettingsPatch{SubsTitle: p.SubsTitle, SubsSupportURL: p.SubsSupportURL})
}
