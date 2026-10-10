package agent

import (
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("SYLPH_CORE_URL", "https://core.example:8443")
	t.Setenv("SYLPH_TLS_CERT", "/certs/agent.pem")
	t.Setenv("SYLPH_TLS_KEY", "/certs/agent.key")
	t.Setenv("SYLPH_CA_CERT", "/certs/ca.pem")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CoreURL != "https://core.example:8443" {
		t.Fatalf("core url = %q", cfg.CoreURL)
	}
	if cfg.ServerName != "core.sylph.internal" {
		t.Fatalf("server name = %q", cfg.ServerName)
	}
	if cfg.CachePath != "cache.json" {
		t.Fatalf("cache = %q", cfg.CachePath)
	}
	if cfg.InboundTag != "vless-in" {
		t.Fatalf("tag = %q", cfg.InboundTag)
	}
	if cfg.SyncInterval != 30*time.Second {
		t.Fatalf("interval = %v", cfg.SyncInterval)
	}
}

func TestLoadConfigMissingRequired(t *testing.T) {
	t.Setenv("SYLPH_CORE_URL", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("want error without core url")
	}
}

func TestLoadConfigCustomInterval(t *testing.T) {
	t.Setenv("SYLPH_CORE_URL", "https://core:8443")
	t.Setenv("SYLPH_TLS_CERT", "a")
	t.Setenv("SYLPH_TLS_KEY", "b")
	t.Setenv("SYLPH_CA_CERT", "c")
	t.Setenv("SYLPH_SYNC_INTERVAL", "15s")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SyncInterval != 15*time.Second {
		t.Fatalf("interval = %v", cfg.SyncInterval)
	}
}
