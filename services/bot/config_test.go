package bot

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SYLPH_BOT_TOKEN", "123456:ABC-secret")
	t.Setenv("SYLPH_CORE_URL", "https://core.sylph.internal:8443")
	t.Setenv("SYLPH_TLS_CERT", "/certs/cert.pem")
	t.Setenv("SYLPH_TLS_KEY", "/certs/key.pem")
	t.Setenv("SYLPH_CA_CERT", "/certs/ca.pem")
	t.Setenv("SYLPH_SUBS_URL", "https://subs.example.com/")
	for _, k := range []string{"SYLPH_CORE_NAME", "SYLPH_TELEGRAM_API", "SYLPH_BOT_WEBHOOK_LISTEN", "SYLPH_BOT_TRUSTED_PROXIES", "SYLPH_BOT_YOOKASSA_IPS", "SYLPH_BOT_TZ", "SYLPH_BOT_CUSTOM_EMOJI"} {
		t.Setenv(k, "")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	setBaseEnv(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "123456:ABC-secret" || cfg.CoreURL != "https://core.sylph.internal:8443" || cfg.ServerName != "core.sylph.internal" ||
		cfg.SubsURL != "https://subs.example.com" || cfg.TelegramAPI != "https://api.telegram.org" || cfg.WebhookListen != "" ||
		len(cfg.TrustedProxies) != 0 || cfg.Location.String() != "Europe/Moscow" || cfg.CustomEmoji {
		t.Fatalf("cfg = %+v", cfg)
	}
	for _, ip := range []string{"185.71.76.1", "185.71.77.30", "77.75.153.100", "77.75.156.11", "77.75.156.35", "77.75.154.200", "2a02:5180::1"} {
		if !inAny(cfg.YooKassaIPs, netip.MustParseAddr(ip)) {
			t.Fatalf("default allowlist misses %s", ip)
		}
	}
	for _, ip := range []string{"185.71.76.32", "77.75.156.12", "8.8.8.8", "2a02:5181::1"} {
		if inAny(cfg.YooKassaIPs, netip.MustParseAddr(ip)) {
			t.Fatalf("default allowlist lets %s in", ip)
		}
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SYLPH_CORE_NAME", "core.example")
	t.Setenv("SYLPH_TELEGRAM_API", "http://127.0.0.1:9000")
	t.Setenv("SYLPH_BOT_WEBHOOK_LISTEN", ":8080")
	t.Setenv("SYLPH_BOT_TRUSTED_PROXIES", "172.18.0.0/16, 10.0.0.5")
	t.Setenv("SYLPH_BOT_YOOKASSA_IPS", "203.0.113.0/24")
	t.Setenv("SYLPH_BOT_TZ", "Asia/Yekaterinburg")
	t.Setenv("SYLPH_BOT_CUSTOM_EMOJI", "true")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CustomEmoji {
		t.Fatal("custom emoji switch ignored")
	}
	if cfg.ServerName != "core.example" || cfg.TelegramAPI != "http://127.0.0.1:9000" || cfg.WebhookListen != ":8080" || cfg.Location.String() != "Asia/Yekaterinburg" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if !inAny(cfg.TrustedProxies, netip.MustParseAddr("172.18.3.4")) || !inAny(cfg.TrustedProxies, netip.MustParseAddr("10.0.0.5")) || inAny(cfg.TrustedProxies, netip.MustParseAddr("10.0.0.6")) {
		t.Fatalf("trusted = %v", cfg.TrustedProxies)
	}
	if !inAny(cfg.YooKassaIPs, netip.MustParseAddr("203.0.113.7")) || inAny(cfg.YooKassaIPs, netip.MustParseAddr("185.71.76.1")) {
		t.Fatalf("allowlist = %v", cfg.YooKassaIPs)
	}
	if cfg.Location.String() != time.Now().In(cfg.Location).Location().String() {
		t.Fatal("location not usable")
	}
}

func TestLoadConfigRejects(t *testing.T) {
	cases := map[string][2]string{
		"no token":      {"SYLPH_BOT_TOKEN", ""},
		"no core url":   {"SYLPH_CORE_URL", ""},
		"no cert":       {"SYLPH_TLS_CERT", ""},
		"no subs url":   {"SYLPH_SUBS_URL", ""},
		"bad subs url":  {"SYLPH_SUBS_URL", "subs.example.com"},
		"subs path":     {"SYLPH_SUBS_URL", "https://subs.example.com/?x=1"},
		"bad telegram":  {"SYLPH_TELEGRAM_API", "ftp://x"},
		"bad proxies":   {"SYLPH_BOT_TRUSTED_PROXIES", "not-an-ip"},
		"bad allowlist": {"SYLPH_BOT_YOOKASSA_IPS", "300.1.1.1"},
		"bad tz":        {"SYLPH_BOT_TZ", "Mars/Olympus"},
		"bad emoji":     {"SYLPH_BOT_CUSTOM_EMOJI", "maybe"},
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

func TestConfigErrorsDoNotCarryTheToken(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SYLPH_SUBS_URL", "")
	_, err := LoadConfig()
	if err == nil || strings.Contains(err.Error(), "ABC-secret") {
		t.Fatalf("err = %v", err)
	}
}
