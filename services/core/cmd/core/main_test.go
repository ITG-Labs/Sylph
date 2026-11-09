package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
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

func TestBotUsernameFromEnv(t *testing.T) {
	t.Setenv("SYLPH_BOT_USERNAME", "")
	if name, err := botUsername(); err != nil || name != "" {
		t.Fatalf("empty = %q, %v", name, err)
	}
	t.Setenv("SYLPH_BOT_USERNAME", "@the_underground_service_bot")
	if name, err := botUsername(); err != nil || name != "the_underground_service_bot" {
		t.Fatalf("with @ = %q, %v", name, err)
	}
	for _, bad := range []string{"bot", "has space", "t.me/bot_name", strings.Repeat("a", 33)} {
		t.Setenv("SYLPH_BOT_USERNAME", bad)
		if _, err := botUsername(); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestHealthHandlerPingsDatabase(t *testing.T) {
	var fail error
	h := healthHandler(func(context.Context) error { return fail }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("healthy = %d %s", rec.Code, rec.Body.String())
	}
	fail = errors.New("db down")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "db down") {
		t.Fatalf("db down = %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/other", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other path = %d", rec.Code)
	}
}

func TestCertNotAfter(t *testing.T) {
	ca, err := mtls.NewCA("Test CA", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, mtls.EncodeCertPEM(ca.Cert), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := certNotAfter(path)
	if err != nil || !got.Equal(ca.Cert.NotAfter) {
		t.Fatalf("not after = %v, %v; want %v", got, err, ca.Cert.NotAfter)
	}
	if _, err := certNotAfter(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestNewHTTPServerBoundsEveryPhase(t *testing.T) {
	srv := newHTTPServer(":0", http.NotFoundHandler(), nil)
	if srv.ReadHeaderTimeout <= 0 || srv.ReadHeaderTimeout > 30*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v, want within (0, 30s]", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout < srv.ReadHeaderTimeout {
		t.Fatalf("ReadTimeout = %v, want at least ReadHeaderTimeout %v", srv.ReadTimeout, srv.ReadHeaderTimeout)
	}
	if srv.WriteTimeout <= 0 {
		t.Fatal("WriteTimeout is not set")
	}
	if srv.IdleTimeout <= 0 {
		t.Fatal("IdleTimeout is not set")
	}
	if srv.MaxHeaderBytes <= 0 {
		t.Fatal("MaxHeaderBytes is not set")
	}
}

func TestStalledHandshakeIsDropped(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler(), &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return nil, errors.New("no certificate") },
	})
	srv.ReadHeaderTimeout = 150 * time.Millisecond
	srv.ReadTimeout = time.Second
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.ServeTLS(ln, "", "")
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("server answered a silent client")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("silent client was still connected after the handshake deadline")
	}
}

func TestMigrateCommandStatusAndForce(t *testing.T) {
	url := os.Getenv("SYLPH_TEST_DB")
	if url == "" {
		t.Skip("SYLPH_TEST_DB not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := migrateCommand(pool, []string{"status"}, &out); err != nil || !strings.Contains(out.String(), "version "+strconv.Itoa(version)) || strings.Contains(out.String(), "dirty") {
		t.Fatalf("clean status = %q, %v", out.String(), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET dirty = true`); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := migrateCommand(pool, []string{"status"}, &out); err != nil || !strings.Contains(out.String(), "dirty") {
		t.Fatalf("dirty status = %q, %v", out.String(), err)
	}
	out.Reset()
	if err := migrateCommand(pool, []string{"force", strconv.Itoa(version - 2)}, &out); err == nil {
		t.Fatal("force two versions back on a dirty database was accepted")
	}
	if err := migrateCommand(pool, []string{"force", "--unsafe", strconv.Itoa(version + 1)}, &out); err == nil {
		t.Fatal("force to an unknown version was accepted")
	}
	if err := migrateCommand(pool, []string{"force", strconv.Itoa(version)}, &out); err != nil {
		t.Fatal(err)
	}
	if st, err := store.MigrationState(pool); err != nil || st.Dirty || int(st.Version) != version {
		t.Fatalf("after force = %+v, %v", st, err)
	}
	if err := migrateCommand(pool, []string{"force", strconv.Itoa(version - 2)}, &out); err == nil || !strings.Contains(err.Error(), "--unsafe") {
		t.Fatalf("force on a clean database: err = %v, want a refusal mentioning --unsafe", err)
	}
	out.Reset()
	if err := migrateCommand(pool, []string{"force", "--unsafe", strconv.Itoa(version - 2)}, &out); err != nil {
		t.Fatalf("unsafe force: %v", err)
	}
	if st, err := store.MigrationState(pool); err != nil || int(st.Version) != version-2 {
		t.Fatalf("after unsafe force = %+v, %v", st, err)
	}
	for _, bad := range [][]string{nil, {"force"}, {"force", "x"}, {"force", "-2"}, {"force", "--unsafe"}, {"force", "--bogus", "3"}, {"drop"}, {"status", "extra"}} {
		if err := migrateCommand(pool, bad, &out); err == nil {
			t.Errorf("args %v accepted", bad)
		}
	}
}
