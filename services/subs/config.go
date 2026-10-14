package subs

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	CoreURL    string
	TLSCert    string
	TLSKey     string
	CACert     string
	ServerName string
	Listen     string
	Settings   Settings
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		CoreURL:    os.Getenv("SYLPH_CORE_URL"),
		TLSCert:    os.Getenv("SYLPH_TLS_CERT"),
		TLSKey:     os.Getenv("SYLPH_TLS_KEY"),
		CACert:     os.Getenv("SYLPH_CA_CERT"),
		ServerName: envOr("SYLPH_CORE_NAME", "core.sylph.internal"),
		Listen:     envOr("SYLPH_LISTEN", ":8080"),
		Settings:   Settings{UpdateHours: 12},
	}
	if cfg.CoreURL == "" {
		return nil, errors.New("SYLPH_CORE_URL is required")
	}
	if cfg.TLSCert == "" || cfg.TLSKey == "" || cfg.CACert == "" {
		return nil, errors.New("SYLPH_TLS_CERT, SYLPH_TLS_KEY and SYLPH_CA_CERT are required")
	}
	if v := os.Getenv("SYLPH_SUBS_UPDATE_HOURS"); v != "" {
		h, err := strconv.Atoi(v)
		if err != nil || h < 1 || h > 168 {
			return nil, errors.New("SYLPH_SUBS_UPDATE_HOURS must be 1-168")
		}
		cfg.Settings.UpdateHours = h
	}
	loc, err := time.LoadLocation(envOr("SYLPH_SUBS_TZ", "Europe/Moscow"))
	if err != nil {
		return nil, fmt.Errorf("SYLPH_SUBS_TZ: %w", err)
	}
	cfg.Settings.Location = loc
	return cfg, nil
}

func DeprecatedEnv() []string {
	var out []string
	for _, k := range []string{"SYLPH_SUBS_TITLE", "SYLPH_SUBS_SUPPORT_URL"} {
		if os.Getenv(k) != "" {
			out = append(out, k)
		}
	}
	return out
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
