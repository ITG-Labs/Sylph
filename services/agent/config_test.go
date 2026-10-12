package agent

import (
	"os"
	"testing"
	"time"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SYLPH_CORE_URL", "https://core:8443")
	t.Setenv("SYLPH_TLS_CERT", "a")
	t.Setenv("SYLPH_TLS_KEY", "b")
	t.Setenv("SYLPH_CA_CERT", "c")
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	unsetEnv(t, "SYLPH_XRAY_CONFIG")
	unsetEnv(t, "SYLPH_NODE_STATE_PATH")
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
	if cfg.StatsPath != "stats.json" {
		t.Fatalf("stats = %q", cfg.StatsPath)
	}
	if cfg.SyncInterval != 30*time.Second {
		t.Fatalf("interval = %v", cfg.SyncInterval)
	}
	if cfg.XrayVersion != "" {
		t.Fatalf("xray version = %q, want empty by default", cfg.XrayVersion)
	}
	if cfg.XrayConfig != "/etc/xray/config.json" || cfg.NodeStatePath != "node.json" {
		t.Fatalf("xray config = %q, node state = %q", cfg.XrayConfig, cfg.NodeStatePath)
	}
}

func TestLoadConfigNodePaths(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SYLPH_XRAY_CONFIG", "/shared/xray.json")
	t.Setenv("SYLPH_NODE_STATE_PATH", "/var/lib/sylph/node.json")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XrayConfig != "/shared/xray.json" || cfg.NodeStatePath != "/var/lib/sylph/node.json" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadConfigXrayVersion(t *testing.T) {
	t.Setenv("SYLPH_CORE_URL", "https://core:8443")
	t.Setenv("SYLPH_TLS_CERT", "a")
	t.Setenv("SYLPH_TLS_KEY", "b")
	t.Setenv("SYLPH_CA_CERT", "c")
	t.Setenv("SYLPH_XRAY_VERSION", "25.9.11")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XrayVersion != "25.9.11" {
		t.Fatalf("xray version = %q", cfg.XrayVersion)
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

func TestLoadConfigXrayDefaults(t *testing.T) {
	setRequiredEnv(t)
	unsetEnv(t, "SYLPH_XRAY_API")
	unsetEnv(t, "SYLPH_XRAY_TIMEOUT")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XrayAPI != "127.0.0.1:10085" {
		t.Fatalf("xray api = %q", cfg.XrayAPI)
	}
	if cfg.XrayTimeout != 5*time.Second {
		t.Fatalf("xray timeout = %v", cfg.XrayTimeout)
	}
}

func TestLoadConfigXrayOverrides(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SYLPH_XRAY_API", "10.0.0.5:10085")
	t.Setenv("SYLPH_XRAY_TIMEOUT", "2s")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XrayAPI != "10.0.0.5:10085" || cfg.XrayTimeout != 2*time.Second {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadConfigRejectsBadXrayTimeout(t *testing.T) {
	setRequiredEnv(t)
	for _, v := range []string{"abc", "0s", "-1s", "5"} {
		t.Setenv("SYLPH_XRAY_TIMEOUT", v)
		if _, err := LoadConfig(); err == nil {
			t.Fatalf("timeout %q accepted", v)
		}
	}
}
