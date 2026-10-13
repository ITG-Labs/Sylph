package panel

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	CoreURL        string
	TLSCert        string
	TLSKey         string
	CACert         string
	ServerName     string
	Listen         string
	AdminsFile     string
	SessionTTL     time.Duration
	TrustedProxies proxyTrust
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		CoreURL:    os.Getenv("SYLPH_CORE_URL"),
		TLSCert:    os.Getenv("SYLPH_TLS_CERT"),
		TLSKey:     os.Getenv("SYLPH_TLS_KEY"),
		CACert:     os.Getenv("SYLPH_CA_CERT"),
		ServerName: envOr("SYLPH_CORE_NAME", "core.sylph.internal"),
		Listen:     envOr("SYLPH_LISTEN", ":8080"),
		AdminsFile: os.Getenv("SYLPH_PANEL_ADMINS_FILE"),
		SessionTTL: 12 * time.Hour,
	}
	u, err := url.Parse(cfg.CoreURL)
	if cfg.CoreURL == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return nil, errors.New("SYLPH_CORE_URL must be https://host[:port]")
	}
	if cfg.TLSCert == "" || cfg.TLSKey == "" || cfg.CACert == "" {
		return nil, errors.New("SYLPH_TLS_CERT, SYLPH_TLS_KEY and SYLPH_CA_CERT are required")
	}
	if cfg.AdminsFile == "" {
		return nil, errors.New("SYLPH_PANEL_ADMINS_FILE is required")
	}
	if v := os.Getenv("SYLPH_PANEL_SESSION_HOURS"); v != "" {
		h, err := strconv.Atoi(v)
		if err != nil || h < 1 || h > 168 {
			return nil, errors.New("SYLPH_PANEL_SESSION_HOURS must be 1-168")
		}
		cfg.SessionTTL = time.Duration(h) * time.Hour
	}
	cfg.TrustedProxies, err = parseTrustedProxies(os.Getenv("SYLPH_PANEL_TRUSTED_PROXIES"))
	if err != nil {
		return nil, fmt.Errorf("SYLPH_PANEL_TRUSTED_PROXIES: %w", err)
	}
	return cfg, nil
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
