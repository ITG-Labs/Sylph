package cron

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	CoreURL       string
	TLSCert       string
	TLSKey        string
	CACert        string
	ServerName    string
	ExpireEvery   time.Duration
	CleanupEvery  time.Duration
	RetentionDays int
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		CoreURL:       os.Getenv("SYLPH_CORE_URL"),
		TLSCert:       os.Getenv("SYLPH_TLS_CERT"),
		TLSKey:        os.Getenv("SYLPH_TLS_KEY"),
		CACert:        os.Getenv("SYLPH_CA_CERT"),
		ServerName:    envOr("SYLPH_CORE_NAME", "core.sylph.internal"),
		ExpireEvery:   time.Minute,
		CleanupEvery:  time.Hour,
		RetentionDays: 30,
	}
	if cfg.CoreURL == "" {
		return nil, errors.New("SYLPH_CORE_URL is required")
	}
	if cfg.TLSCert == "" || cfg.TLSKey == "" || cfg.CACert == "" {
		return nil, errors.New("SYLPH_TLS_CERT, SYLPH_TLS_KEY and SYLPH_CA_CERT are required")
	}
	var err error
	if cfg.ExpireEvery, err = interval("SYLPH_CRON_EXPIRE_EVERY", cfg.ExpireEvery); err != nil {
		return nil, err
	}
	if cfg.CleanupEvery, err = interval("SYLPH_CRON_CLEANUP_EVERY", cfg.CleanupEvery); err != nil {
		return nil, err
	}
	if v := os.Getenv("SYLPH_CRON_RETENTION_DAYS"); v != "" {
		days, err := strconv.Atoi(v)
		if err != nil || days < 1 || days > 3650 {
			return nil, errors.New("SYLPH_CRON_RETENTION_DAYS must be 1-3650")
		}
		cfg.RetentionDays = days
	}
	return cfg, nil
}

func interval(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < time.Second {
		return 0, errors.New(key + " must be a Go duration of at least 1s")
	}
	return d, nil
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
