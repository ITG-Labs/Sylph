package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

var cliNow = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func TestInitCA(t *testing.T) {
	dir := t.TempDir()
	if err := initCA(dir, "Sylph CA", cliNow); err != nil {
		t.Fatal(err)
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := mtls.ParseCertPEM(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.IsCA {
		t.Fatal("root cert should be CA")
	}
	keyStat, err := os.Stat(filepath.Join(dir, "ca.key"))
	if err != nil {
		t.Fatal(err)
	}
	if keyStat.Mode().Perm() != 0o600 {
		t.Fatalf("ca.key mode = %v, want 0600", keyStat.Mode().Perm())
	}
	loaded, err := loadCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Cert.SerialNumber.Cmp(cert.SerialNumber) != 0 {
		t.Fatal("loadCA serial mismatch")
	}
}

func TestInitCATwiceFails(t *testing.T) {
	dir := t.TempDir()
	if err := initCA(dir, "Sylph CA", cliNow); err != nil {
		t.Fatal(err)
	}
	if err := initCA(dir, "Sylph CA", cliNow); err == nil {
		t.Fatal("second init should fail")
	}
}

func TestIssueCert(t *testing.T) {
	dir := t.TempDir()
	if err := initCA(dir, "Sylph CA", cliNow); err != nil {
		t.Fatal(err)
	}
	if err := issueCert(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, cliNow); err != nil {
		t.Fatal(err)
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, "certs", "bot", "cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := mtls.ParseCertPEM(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "bot" {
		t.Fatalf("cn = %q, want bot", cert.Subject.CommonName)
	}
	if role, ok := mtls.RoleOf(cert); !ok || role != mtls.RoleBot {
		t.Fatalf("role = %q, %v; want bot", role, ok)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, "certs", "bot", "key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mtls.ParseKeyPEM(keyPEM); err != nil {
		t.Fatal(err)
	}
	keyStat, err := os.Stat(filepath.Join(dir, "certs", "bot", "key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if keyStat.Mode().Perm() != 0o600 {
		t.Fatalf("key.pem mode = %v, want 0600", keyStat.Mode().Perm())
	}
}

func TestIssueCertTwiceFails(t *testing.T) {
	dir := t.TempDir()
	if err := initCA(dir, "Sylph CA", cliNow); err != nil {
		t.Fatal(err)
	}
	if err := issueCert(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, cliNow); err != nil {
		t.Fatal(err)
	}
	if err := issueCert(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, cliNow); err == nil {
		t.Fatal("second issue should fail")
	}
}

func TestRevokeCert(t *testing.T) {
	dir := t.TempDir()
	if err := initCA(dir, "Sylph CA", cliNow); err != nil {
		t.Fatal(err)
	}
	if err := issueCert(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, cliNow); err != nil {
		t.Fatal(err)
	}
	if err := revokeCert(dir, "bot"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "revoked.json"))
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, "certs", "bot", "cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := mtls.ParseCertPEM(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"serials":["%s"]}`, cert.SerialNumber.String())
	got := strings.TrimSpace(string(data))
	if got != want {
		t.Fatalf("revoked.json = %s, want %s", got, want)
	}
	if err := revokeCert(dir, "bot"); err == nil {
		t.Fatal("double revoke should fail")
	}
	if err := revokeCert(dir, "missing"); err == nil {
		t.Fatal("revoke of missing cert should fail")
	}
}

func TestListCerts(t *testing.T) {
	dir := t.TempDir()
	if err := initCA(dir, "Sylph CA", cliNow); err != nil {
		t.Fatal(err)
	}
	if err := issueCert(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, cliNow); err != nil {
		t.Fatal(err)
	}
	if err := issueCert(dir, "core", mtls.ServerUsage, "", 24*time.Hour, cliNow); err != nil {
		t.Fatal(err)
	}
	infos, err := listCerts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("len = %d, want 2", len(infos))
	}
	if infos[0].Name != "bot" || infos[0].Type != "client" || infos[0].Role != mtls.RoleBot {
		t.Fatalf("infos[0] = %+v, want bot/client/bot", infos[0])
	}
	if infos[1].Name != "core" || infos[1].Type != "server" || infos[1].Role != "" {
		t.Fatalf("infos[1] = %+v, want core/server without role", infos[1])
	}
	if infos[0].Serial == "" {
		t.Fatal("serial should not be empty")
	}
}

func TestIssueCertRoleValidation(t *testing.T) {
	dir := t.TempDir()
	if err := initCA(dir, "Sylph CA", cliNow); err != nil {
		t.Fatal(err)
	}
	if err := issueCert(dir, "bot", mtls.ClientUsage, "", 24*time.Hour, cliNow); err == nil {
		t.Fatal("client cert without role should fail")
	}
	if err := issueCert(dir, "bot", mtls.ClientUsage, "admin", 24*time.Hour, cliNow); err == nil {
		t.Fatal("client cert with unknown role should fail")
	}
	if err := issueCert(dir, "core", mtls.ServerUsage, mtls.RolePanel, 24*time.Hour, cliNow); err == nil {
		t.Fatal("server cert with role should fail")
	}
	if _, err := listCerts(dir); err != nil {
		t.Fatalf("failed issue left a broken cert dir: %v", err)
	}
	if err := issueCert(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, cliNow); err != nil {
		t.Fatalf("issue after failed attempt: %v", err)
	}
}
