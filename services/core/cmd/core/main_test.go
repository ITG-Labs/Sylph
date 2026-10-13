package main

import (
	"crypto/x509"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRevokedList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.json")
	if err := os.WriteFile(path, []byte(`{"serials":["123","456"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	list := loadRevokedList(path)
	if !list.IsRevoked(fakeCert("123")) {
		t.Fatal("serial 123 should be revoked")
	}
	if list.IsRevoked(fakeCert("999")) {
		t.Fatal("serial 999 should not be revoked")
	}
}

func TestLoadRevokedListMissing(t *testing.T) {
	list := loadRevokedList(filepath.Join(t.TempDir(), "missing.json"))
	if list == nil {
		t.Fatal("missing file should give empty list, not nil")
	}
	if list.IsRevoked(fakeCert("123")) {
		t.Fatal("empty list should not revoke anything")
	}
}

func fakeCert(serial string) *x509.Certificate {
	n, ok := new(big.Int).SetString(serial, 10)
	if !ok {
		panic("bad serial")
	}
	return &x509.Certificate{SerialNumber: n}
}

func TestPaymentsConfigFromEnv(t *testing.T) {
	for _, k := range []string{"SYLPH_YOOKASSA_SHOP_ID", "SYLPH_YOOKASSA_SECRET_KEY", "SYLPH_YOOKASSA_RETURN_URL", "SYLPH_YOOKASSA_API"} {
		t.Setenv(k, "")
	}
	cfg, err := paymentsConfig()
	if err != nil || cfg != nil {
		t.Fatalf("no env = %+v, %v; want disabled", cfg, err)
	}
	t.Setenv("SYLPH_YOOKASSA_SHOP_ID", "1352926")
	if _, err := paymentsConfig(); err == nil {
		t.Fatal("shop id alone must fail")
	}
	t.Setenv("SYLPH_YOOKASSA_SECRET_KEY", "test_secret")
	if _, err := paymentsConfig(); err == nil {
		t.Fatal("missing return url must fail")
	}
	t.Setenv("SYLPH_YOOKASSA_RETURN_URL", "t.me/bot")
	if _, err := paymentsConfig(); err == nil {
		t.Fatal("return url without scheme must fail")
	}
	t.Setenv("SYLPH_YOOKASSA_RETURN_URL", "https://t.me/sylph_bot")
	cfg, err = paymentsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ShopID != "1352926" || cfg.SecretKey != "test_secret" || cfg.ReturnURL != "https://t.me/sylph_bot" || cfg.API != "https://api.yookassa.ru/v3" {
		t.Fatalf("cfg = %+v", cfg)
	}
	t.Setenv("SYLPH_YOOKASSA_API", "ftp://x")
	if _, err := paymentsConfig(); err == nil {
		t.Fatal("non-http api url must fail")
	}
	t.Setenv("SYLPH_YOOKASSA_API", "http://127.0.0.1:9999/v3")
	if cfg, err = paymentsConfig(); err != nil || cfg.API != "http://127.0.0.1:9999/v3" {
		t.Fatalf("custom api = %+v, %v", cfg, err)
	}
	t.Setenv("SYLPH_YOOKASSA_SECRET_KEY", "")
	if _, err := paymentsConfig(); err == nil {
		t.Fatal("shop id and return url without the secret must fail")
	}
	t.Setenv("SYLPH_YOOKASSA_SECRET_KEY", "test_secret")
	t.Setenv("SYLPH_YOOKASSA_SHOP_ID", "")
	if _, err := paymentsConfig(); err == nil {
		t.Fatal("secret without shop id must fail")
	}
}

func TestStatsTimezoneFromEnv(t *testing.T) {
	t.Setenv("SYLPH_STATS_TZ", "")
	if tz, err := statsTimezone(); err != nil || tz != "Europe/Moscow" {
		t.Fatalf("default = %q, %v", tz, err)
	}
	t.Setenv("SYLPH_STATS_TZ", "Asia/Tokyo")
	if tz, err := statsTimezone(); err != nil || tz != "Asia/Tokyo" {
		t.Fatalf("tokyo = %q, %v", tz, err)
	}
	t.Setenv("SYLPH_STATS_TZ", "Mars/Olympus")
	if _, err := statsTimezone(); err == nil {
		t.Fatal("unknown timezone accepted")
	}
}

func TestNodeThresholdsFromEnv(t *testing.T) {
	t.Setenv("SYLPH_NODE_OFFLINE_AFTER", "")
	t.Setenv("SYLPH_NODE_SYNC_STALE_AFTER", "")
	offline, stale, err := nodeThresholds()
	if err != nil || offline != time.Minute || stale != 3*time.Minute {
		t.Fatalf("defaults = %v %v %v", offline, stale, err)
	}
	t.Setenv("SYLPH_NODE_OFFLINE_AFTER", "90s")
	t.Setenv("SYLPH_NODE_SYNC_STALE_AFTER", "10m")
	if offline, stale, err := nodeThresholds(); err != nil || offline != 90*time.Second || stale != 10*time.Minute {
		t.Fatalf("custom = %v %v %v", offline, stale, err)
	}
	for _, bad := range []string{"0s", "-1m", "soon"} {
		t.Setenv("SYLPH_NODE_OFFLINE_AFTER", bad)
		if _, _, err := nodeThresholds(); err == nil {
			t.Fatalf("offline %q accepted", bad)
		}
	}
	t.Setenv("SYLPH_NODE_OFFLINE_AFTER", "1m")
	t.Setenv("SYLPH_NODE_SYNC_STALE_AFTER", "0")
	if _, _, err := nodeThresholds(); err == nil {
		t.Fatal("zero stale accepted")
	}
}
