package main

import (
	"os"
	"path/filepath"
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
	if err := issueCert(dir, "bot", mtls.ClientUsage, 24*time.Hour, cliNow); err != nil {
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
	if err := issueCert(dir, "bot", mtls.ClientUsage, 24*time.Hour, cliNow); err != nil {
		t.Fatal(err)
	}
	if err := issueCert(dir, "bot", mtls.ClientUsage, 24*time.Hour, cliNow); err == nil {
		t.Fatal("second issue should fail")
	}
}
