package cron

import (
	"strings"
	"testing"
	"time"
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
		ServerName: "core.sylph.internal", ExpireEvery: time.Minute, CleanupEvery: time.Hour, PaymentsEvery: time.Minute, RetentionDays: 30,
		StatsRetentionDays: 365,
	}
	if *cfg != want {
		t.Fatalf("cfg = %+v", *cfg)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SYLPH_CORE_NAME", "core.example")
	t.Setenv("SYLPH_CRON_EXPIRE_EVERY", "30s")
	t.Setenv("SYLPH_CRON_CLEANUP_EVERY", "24h")
	t.Setenv("SYLPH_CRON_RETENTION_DAYS", "90")
	t.Setenv("SYLPH_CRON_PAYMENTS_EVERY", "2m")
	t.Setenv("SYLPH_CRON_STATS_RETENTION_DAYS", "730")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "core.example" || cfg.ExpireEvery != 30*time.Second || cfg.CleanupEvery != 24*time.Hour || cfg.RetentionDays != 90 || cfg.PaymentsEvery != 2*time.Minute || cfg.StatsRetentionDays != 730 {
		t.Fatalf("cfg = %+v", *cfg)
	}
}

func TestLoadConfigRejects(t *testing.T) {
	cases := map[string][2]string{
		"no core url":       {"SYLPH_CORE_URL", ""},
		"no cert":           {"SYLPH_TLS_CERT", ""},
		"bad expire":        {"SYLPH_CRON_EXPIRE_EVERY", "often"},
		"too fast expire":   {"SYLPH_CRON_EXPIRE_EVERY", "500ms"},
		"too slow expire":   {"SYLPH_CRON_EXPIRE_EVERY", "5m"},
		"bad cleanup":       {"SYLPH_CRON_CLEANUP_EVERY", "-1h"},
		"bad payments":      {"SYLPH_CRON_PAYMENTS_EVERY", "soon"},
		"zero retention":    {"SYLPH_CRON_RETENTION_DAYS", "0"},
		"huge retention":    {"SYLPH_CRON_RETENTION_DAYS", "3651"},
		"retention as text": {"SYLPH_CRON_RETENTION_DAYS", "month"},
		"zero stats":        {"SYLPH_CRON_STATS_RETENTION_DAYS", "0"},
		"huge stats":        {"SYLPH_CRON_STATS_RETENTION_DAYS", "3651"},
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
