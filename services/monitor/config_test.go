package monitor

import (
	"strings"
	"testing"
	"time"
)

func setBase(t *testing.T) {
	t.Helper()
	t.Setenv("SYLPH_CORE_URL", "https://core.example:443")
	t.Setenv("SYLPH_TLS_CERT", "/certs/cert.pem")
	t.Setenv("SYLPH_TLS_KEY", "/certs/key.pem")
	t.Setenv("SYLPH_CA_CERT", "/certs/ca.pem")
	t.Setenv("SYLPH_MONITOR_BOT_TOKEN", "123:secret")
	t.Setenv("SYLPH_MONITOR_CHAT_IDS", "111, 222")
}

func TestLoadConfigDefaults(t *testing.T) {
	setBase(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ChatIDs) != 2 || cfg.ChatIDs[0] != 111 || cfg.ChatIDs[1] != 222 {
		t.Fatalf("chats = %v", cfg.ChatIDs)
	}
	if cfg.Interval != time.Minute || cfg.StatePath != "state.json" || cfg.Location.String() != "Europe/Moscow" ||
		cfg.TelegramAPI != "https://api.telegram.org" || cfg.ServerName != "core.sylph.internal" || cfg.PanelURL != "" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadConfigRejectsBadValues(t *testing.T) {
	cases := map[string][2]string{
		"no token":     {"SYLPH_MONITOR_BOT_TOKEN", ""},
		"no chats":     {"SYLPH_MONITOR_CHAT_IDS", ""},
		"bad chat":     {"SYLPH_MONITOR_CHAT_IDS", "111,abc"},
		"bad interval": {"SYLPH_MONITOR_INTERVAL", "1s"},
		"bad tz":       {"SYLPH_MONITOR_TZ", "Mars/Olympus"},
		"bad panel":    {"SYLPH_MONITOR_PANEL_URL", "panel.example.com"},
		"no core":      {"SYLPH_CORE_URL", ""},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			setBase(t)
			t.Setenv(kv[0], kv[1])
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("%s=%q accepted", kv[0], kv[1])
			} else if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaks the token: %v", err)
			}
		})
	}
}

func TestLoadConfigPanelURLTrimsSlash(t *testing.T) {
	setBase(t)
	t.Setenv("SYLPH_MONITOR_PANEL_URL", "https://panel.example.com/")
	cfg, err := LoadConfig()
	if err != nil || cfg.PanelURL != "https://panel.example.com" {
		t.Fatalf("panel = %q, %v", cfg.PanelURL, err)
	}
}
