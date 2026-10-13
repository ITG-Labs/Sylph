package panel

import (
	"testing"
	"time"
)

func setRequired(t *testing.T) {
	t.Setenv("SYLPH_CORE_URL", "https://core.example:8443")
	t.Setenv("SYLPH_TLS_CERT", "/c.pem")
	t.Setenv("SYLPH_TLS_KEY", "/k.pem")
	t.Setenv("SYLPH_CA_CERT", "/ca.pem")
	t.Setenv("SYLPH_PANEL_ADMINS_FILE", "/admins.json")
	for _, k := range []string{"SYLPH_CORE_NAME", "SYLPH_LISTEN", "SYLPH_PANEL_SESSION_HOURS", "SYLPH_PANEL_TRUSTED_PROXIES"} {
		t.Setenv(k, "")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	setRequired(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "core.sylph.internal" || cfg.Listen != ":8080" || cfg.SessionTTL != 12*time.Hour || len(cfg.TrustedProxies) != 2 || cfg.AdminsFile != "/admins.json" {
		t.Fatalf("defaults %+v", cfg)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	setRequired(t)
	t.Setenv("SYLPH_PANEL_SESSION_HOURS", "168")
	t.Setenv("SYLPH_PANEL_TRUSTED_PROXIES", "172.18.0.0/16")
	t.Setenv("SYLPH_LISTEN", "127.0.0.1:9000")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionTTL != 168*time.Hour || len(cfg.TrustedProxies) != 1 || cfg.Listen != "127.0.0.1:9000" {
		t.Fatalf("overrides %+v", cfg)
	}
}

func TestLoadConfigRejects(t *testing.T) {
	for name, kv := range map[string][2]string{
		"no core":       {"SYLPH_CORE_URL", ""},
		"ftp core":      {"SYLPH_CORE_URL", "ftp://core"},
		"core path":     {"SYLPH_CORE_URL", "https://core/x?y=1"},
		"no cert":       {"SYLPH_TLS_CERT", ""},
		"no key":        {"SYLPH_TLS_KEY", ""},
		"no ca":         {"SYLPH_CA_CERT", ""},
		"no admins":     {"SYLPH_PANEL_ADMINS_FILE", ""},
		"zero hours":    {"SYLPH_PANEL_SESSION_HOURS", "0"},
		"too long":      {"SYLPH_PANEL_SESSION_HOURS", "169"},
		"not a number":  {"SYLPH_PANEL_SESSION_HOURS", "12h"},
		"bad proxy":     {"SYLPH_PANEL_TRUSTED_PROXIES", "10.0.0.1"},
		"proxy garbage": {"SYLPH_PANEL_TRUSTED_PROXIES", "x"},
	} {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			t.Setenv(kv[0], kv[1])
			if _, err := LoadConfig(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
