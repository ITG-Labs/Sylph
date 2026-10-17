package monitor

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	CoreURL     string
	TLSCert     string
	TLSKey      string
	CACert      string
	ServerName  string
	BotToken    string
	ChatIDs     []int64
	PanelURL    string
	StatePath   string
	Interval    time.Duration
	Location    *time.Location
	TelegramAPI string
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		CoreURL:     os.Getenv("SYLPH_CORE_URL"),
		TLSCert:     os.Getenv("SYLPH_TLS_CERT"),
		TLSKey:      os.Getenv("SYLPH_TLS_KEY"),
		CACert:      os.Getenv("SYLPH_CA_CERT"),
		ServerName:  envOr("SYLPH_CORE_NAME", "core.sylph.internal"),
		BotToken:    os.Getenv("SYLPH_MONITOR_BOT_TOKEN"),
		StatePath:   envOr("SYLPH_MONITOR_STATE_PATH", "state.json"),
		TelegramAPI: strings.TrimRight(envOr("SYLPH_TELEGRAM_API", "https://api.telegram.org"), "/"),
	}
	if cfg.CoreURL == "" {
		return nil, errors.New("SYLPH_CORE_URL is required")
	}
	if cfg.TLSCert == "" || cfg.TLSKey == "" || cfg.CACert == "" {
		return nil, errors.New("SYLPH_TLS_CERT, SYLPH_TLS_KEY and SYLPH_CA_CERT are required")
	}
	if cfg.BotToken == "" {
		return nil, errors.New("SYLPH_MONITOR_BOT_TOKEN is required")
	}
	for _, part := range strings.Split(os.Getenv("SYLPH_MONITOR_CHAT_IDS"), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("SYLPH_MONITOR_CHAT_IDS: %q is not a Telegram ID", part)
		}
		cfg.ChatIDs = append(cfg.ChatIDs, id)
	}
	if len(cfg.ChatIDs) == 0 {
		return nil, errors.New("SYLPH_MONITOR_CHAT_IDS needs at least one Telegram ID")
	}
	if raw := os.Getenv("SYLPH_MONITOR_PANEL_URL"); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, errors.New("SYLPH_MONITOR_PANEL_URL must be an http(s) URL")
		}
		cfg.PanelURL = strings.TrimRight(raw, "/")
	}
	every, err := time.ParseDuration(envOr("SYLPH_MONITOR_INTERVAL", "1m"))
	if err != nil || every < 10*time.Second {
		return nil, errors.New("SYLPH_MONITOR_INTERVAL must be a Go duration of at least 10s")
	}
	cfg.Interval = every
	if cfg.Location, err = time.LoadLocation(envOr("SYLPH_MONITOR_TZ", "Europe/Moscow")); err != nil {
		return nil, errors.New("SYLPH_MONITOR_TZ must be an IANA time zone")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
