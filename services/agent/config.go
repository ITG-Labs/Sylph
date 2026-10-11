package agent

import (
	"errors"
	"os"
	"time"
)

type Config struct {
	CoreURL      string
	TLSCert      string
	TLSKey       string
	CACert       string
	ServerName   string
	CachePath    string
	StatsPath    string
	InboundTag   string
	ListenAPI    string
	XrayVersion  string
	SyncInterval time.Duration
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		CoreURL:      os.Getenv("SYLPH_CORE_URL"),
		TLSCert:      os.Getenv("SYLPH_TLS_CERT"),
		TLSKey:       os.Getenv("SYLPH_TLS_KEY"),
		CACert:       os.Getenv("SYLPH_CA_CERT"),
		ServerName:   envOr("SYLPH_CORE_NAME", "core.sylph.internal"),
		CachePath:    envOr("SYLPH_CACHE_PATH", "cache.json"),
		StatsPath:    envOr("SYLPH_STATS_PATH", "stats.json"),
		InboundTag:   envOr("SYLPH_INBOUND_TAG", "vless-in"),
		ListenAPI:    envOr("SYLPH_LISTEN_API", ""),
		XrayVersion:  os.Getenv("SYLPH_XRAY_VERSION"),
		SyncInterval: 30 * time.Second,
	}
	if cfg.CoreURL == "" {
		return nil, errors.New("SYLPH_CORE_URL is required")
	}
	if cfg.TLSCert == "" || cfg.TLSKey == "" || cfg.CACert == "" {
		return nil, errors.New("SYLPH_TLS_CERT, SYLPH_TLS_KEY and SYLPH_CA_CERT are required")
	}
	if v := os.Getenv("SYLPH_SYNC_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, errors.New("SYLPH_SYNC_INTERVAL: " + err.Error())
		}
		cfg.SyncInterval = d
	}
	return cfg, nil
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
