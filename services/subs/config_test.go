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
		Settings: Settings{UpdateHours: 12},
	}
	if cfg.Settings.Location == nil || cfg.Settings.Location.String() != "Europe/Moscow" {
		t.Fatalf("location = %v", cfg.Settings.Location)
	}
	cfg.Settings.Location = nil
	if *cfg != want {
		t.Fatalf("cfg = %+v", *cfg)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SYLPH_CORE_NAME", "core.example")
	t.Setenv("SYLPH_LISTEN", "127.0.0.1:9090")
	t.Setenv("SYLPH_SUBS_UPDATE_HOURS", "6")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Settings.Location = nil
	if cfg.ServerName != "core.example" || cfg.Listen != "127.0.0.1:9090" || cfg.Settings != (Settings{UpdateHours: 6}) {
		t.Fatalf("cfg = %+v", *cfg)
	}
}

func TestLoadConfigRejects(t *testing.T) {
	cases := map[string][2]string{
		"no core url":      {"SYLPH_CORE_URL", ""},
		"no cert":          {"SYLPH_TLS_CERT", ""},
		"no key":           {"SYLPH_TLS_KEY", ""},
		"no ca":            {"SYLPH_CA_CERT", ""},
		"hours not number": {"SYLPH_SUBS_UPDATE_HOURS", "12h"},
		"hours zero":       {"SYLPH_SUBS_UPDATE_HOURS", "0"},
		"hours too big":    {"SYLPH_SUBS_UPDATE_HOURS", "169"},
		"unknown timezone": {"SYLPH_SUBS_TZ", "Mars/Olympus"},
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

func TestLoadConfigTimezone(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SYLPH_SUBS_TZ", "Asia/Tokyo")
	cfg, err := LoadConfig()
	if err != nil || cfg.Settings.Location.String() != "Asia/Tokyo" {
		t.Fatalf("cfg = %+v, %v", cfg, err)
	}
}

func TestDeprecatedEnvIsReported(t *testing.T) {
	setBaseEnv(t)
	if got := DeprecatedEnv(); len(got) != 0 {
		t.Fatalf("clean env = %v", got)
	}
	t.Setenv("SYLPH_SUBS_TITLE", "Old")
	t.Setenv("SYLPH_SUBS_SUPPORT_URL", "https://t.me/old")
	if got := DeprecatedEnv(); len(got) != 2 || got[0] != "SYLPH_SUBS_TITLE" || got[1] != "SYLPH_SUBS_SUPPORT_URL" {
		t.Fatalf("deprecated = %v", got)
	}
}
