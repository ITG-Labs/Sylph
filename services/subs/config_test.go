package subs

import (
	"strings"
	"testing"
)

func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SYLPH_CORE_URL", "https://core.sylph.internal:8443")
	t.Setenv("SYLPH_TLS_CERT", "/certs/cert.pem")
	t.Setenv("SYLPH_TLS_KEY", "/certs/key.pem")
	t.Setenv("SYLPH_CA_CERT", "/certs/ca.pem")
}

func TestLoadConfigDefaults(t *testing.T) {
	setBaseEnv(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		CoreURL: "https://core.sylph.internal:8443", TLSCert: "/certs/cert.pem", TLSKey: "/certs/key.pem", CACert: "/certs/ca.pem",
		ServerName: "core.sylph.internal", Listen: ":8080",
		Settings: Settings{Title: "Sylph", UpdateHours: 12},
	}
	if *cfg != want {
		t.Fatalf("cfg = %+v", *cfg)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SYLPH_CORE_NAME", "core.example")
	t.Setenv("SYLPH_LISTEN", "127.0.0.1:9090")
	t.Setenv("SYLPH_SUBS_TITLE", "Мой VPN")
	t.Setenv("SYLPH_SUBS_UPDATE_HOURS", "6")
	t.Setenv("SYLPH_SUBS_SUPPORT_URL", "https://t.me/support")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "core.example" || cfg.Listen != "127.0.0.1:9090" || cfg.Settings != (Settings{Title: "Мой VPN", UpdateHours: 6, SupportURL: "https://t.me/support"}) {
		t.Fatalf("cfg = %+v", *cfg)
	}
}

func TestLoadConfigRejects(t *testing.T) {
	cases := map[string][2]string{
		"no core url":        {"SYLPH_CORE_URL", ""},
		"no cert":            {"SYLPH_TLS_CERT", ""},
		"no key":             {"SYLPH_TLS_KEY", ""},
		"no ca":              {"SYLPH_CA_CERT", ""},
		"hours not number":   {"SYLPH_SUBS_UPDATE_HOURS", "12h"},
		"hours zero":         {"SYLPH_SUBS_UPDATE_HOURS", "0"},
		"hours too big":      {"SYLPH_SUBS_UPDATE_HOURS", "169"},
		"support not url":    {"SYLPH_SUBS_SUPPORT_URL", "t.me/support"},
		"support bad scheme": {"SYLPH_SUBS_SUPPORT_URL", "javascript:alert(1)"},
		"support ftp":        {"SYLPH_SUBS_SUPPORT_URL", "ftp://files.example.com/help"},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			setBaseEnv(t)
			t.Setenv(kv[0], kv[1])
			if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), kv[0]) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
