package cron

import (
	"errors"
	"os"
	"strconv"
	"time"
)

const maxExpireEvery = 4 * time.Minute

type Config struct {
	CoreURL       string
	TLSCert       string
	TLSKey        string
	CACert        string
	ServerName    string
	ExpireEvery   time.Duration
	CleanupEvery  time.Duration
	PaymentsEvery time.Duration
	RetentionDays int

	StatsRetentionDays int
	IPRetentionDays    int
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
		PaymentsEvery: time.Minute,
		RetentionDays: 30,

		StatsRetentionDays: 365,
		IPRetentionDays:    30,
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
	if cfg.ExpireEvery > maxExpireEvery {
		return nil, errors.New("SYLPH_CRON_EXPIRE_EVERY must be at most 4m: the monitor alerts when expiry has not run for 5 minutes")
	}
	if cfg.CleanupEvery, err = interval("SYLPH_CRON_CLEANUP_EVERY", cfg.CleanupEvery); err != nil {
		return nil, err
	}
	if cfg.PaymentsEvery, err = interval("SYLPH_CRON_PAYMENTS_EVERY", cfg.PaymentsEvery); err != nil {
		return nil, err
	}
	if cfg.RetentionDays, err = days("SYLPH_CRON_RETENTION_DAYS", cfg.RetentionDays); err != nil {
		return nil, err
	}
	if cfg.IPRetentionDays, err = days("SYLPH_CRON_IP_RETENTION_DAYS", cfg.IPRetentionDays); err != nil {
		return nil, err
	}
	if cfg.StatsRetentionDays, err = days("SYLPH_CRON_STATS_RETENTION_DAYS", cfg.StatsRetentionDays); err != nil {
		return nil, err
	}
	return cfg, nil
}

func days(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 3650 {
		return 0, errors.New(key + " must be 1-3650")
	}
	return n, nil
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
