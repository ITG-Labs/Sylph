package castore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

var testNow = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func TestInitCA(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
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
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Cert.SerialNumber.Cmp(cert.SerialNumber) != 0 {
		t.Fatal("loadCA serial mismatch")
	}
}

func TestInitCATwiceFails(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Init(dir, "Sylph CA", testNow); err == nil {
		t.Fatal("second init should fail")
	}
}

func TestIssueCert(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, testNow); err != nil {
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
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, testNow); err == nil {
		t.Fatal("second issue should fail")
	}
}

func TestRevokeCert(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, testNow); err != nil {
		t.Fatal(err)
	}
	if err := Revoke(dir, "bot"); err != nil {
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
	if err := Revoke(dir, "bot"); err == nil {
		t.Fatal("double revoke should fail")
	}
	if err := Revoke(dir, "missing"); err == nil {
		t.Fatal("revoke of missing cert should fail")
	}
}

func TestListCerts(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "core", mtls.ServerUsage, "", 24*time.Hour, testNow); err != nil {
		t.Fatal(err)
	}
	infos, err := List(dir)
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
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, "", 24*time.Hour, testNow); err == nil {
		t.Fatal("client cert without role should fail")
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, "admin", 24*time.Hour, testNow); err == nil {
		t.Fatal("client cert with unknown role should fail")
	}
	if err := Issue(dir, "core", mtls.ServerUsage, mtls.RolePanel, 24*time.Hour, testNow); err == nil {
		t.Fatal("server cert with role should fail")
	}
	if _, err := List(dir); err != nil {
		t.Fatalf("failed issue left a broken cert dir: %v", err)
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, testNow); err != nil {
		t.Fatalf("issue after failed attempt: %v", err)
	}
}

func TestNameValidation(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "../x", "a/b", ".hidden", "a..b", "a b"} {
		if err := Issue(dir, name, mtls.ClientUsage, mtls.RoleNode, 24*time.Hour, testNow); err == nil {
			t.Fatalf("issue %q should fail", name)
		}
		if _, err := HasLeaf(dir, name); err == nil {
			t.Fatalf("HasLeaf %q should fail", name)
		}
	}
	for _, name := range []string{"core.sylph.internal", "node-de-1", "panel"} {
		if err := ValidName(name); err != nil {
			t.Fatalf("ValidName %q: %v", name, err)
		}
	}
}

func TestExistsAndHasLeaf(t *testing.T) {
	dir := t.TempDir()
	if ok, err := Exists(dir); err != nil || ok {
		t.Fatalf("Exists on empty dir = %v, %v", ok, err)
	}
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if ok, err := Exists(dir); err != nil || !ok {
		t.Fatalf("Exists after init = %v, %v", ok, err)
	}
	if ok, err := HasLeaf(dir, "bot"); err != nil || ok {
		t.Fatalf("HasLeaf before issue = %v, %v", ok, err)
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, testNow); err != nil {
		t.Fatal(err)
	}
	if ok, err := HasLeaf(dir, "bot"); err != nil || !ok {
		t.Fatalf("HasLeaf after issue = %v, %v", ok, err)
	}
}

func TestRenewIssuesFreshLeafAndArchivesOld(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "node-1", mtls.ClientUsage, mtls.RoleNode, 24*time.Hour, testNow); err != nil {
		t.Fatal(err)
	}
	old, err := Inspect(dir, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	oldPEM, err := os.ReadFile(filepath.Join(dir, "certs", "node-1", "cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Revoke(dir, "node-1"); err != nil {
		t.Fatal(err)
	}
	later := testNow.Add(48 * time.Hour)
	if err := Renew(dir, "node-1", 365*24*time.Hour, later); err != nil {
		t.Fatal(err)
	}
	fresh, err := Inspect(dir, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Serial == old.Serial {
		t.Fatal("renewed leaf kept the old serial")
	}
	if fresh.Role != mtls.RoleNode || fresh.Type != "client" {
		t.Fatalf("renewed leaf = %+v, want node client", fresh)
	}
	if !fresh.NotAfter.After(old.NotAfter) {
		t.Fatalf("not after %v, want later than %v", fresh.NotAfter, old.NotAfter)
	}
	if revoked, err := IsRevoked(dir, fresh.Serial); err != nil || revoked {
		t.Fatalf("new leaf revoked = %v, %v", revoked, err)
	}
	if revoked, err := IsRevoked(dir, old.Serial); err != nil || !revoked {
		t.Fatalf("old serial must stay revoked: %v, %v", revoked, err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "archive", "node-1-*", "cert.pem"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("archive = %v, %v", matches, err)
	}
	archived, err := os.ReadFile(matches[0])
	if err != nil || string(archived) != string(oldPEM) {
		t.Fatalf("archived cert differs from the old one: %v", err)
	}
	infos, err := List(dir)
	if err != nil || len(infos) != 1 {
		t.Fatalf("list = %v, %v", infos, err)
	}
}

func TestRenewKeepsServerLeafHostname(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "core.sylph.internal", mtls.ServerUsage, "", 24*time.Hour, testNow); err != nil {
		t.Fatal(err)
	}
	if err := Renew(dir, "core.sylph.internal", 24*time.Hour, testNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, "certs", "core.sylph.internal", "cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := mtls.ParseCertPEM(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname("core.sylph.internal"); err != nil {
		t.Fatal(err)
	}
	info, err := Inspect(dir, "core.sylph.internal")
	if err != nil || info.Type != "server" || info.Role != "" {
		t.Fatalf("info = %+v, %v", info, err)
	}
}

func TestRenewTwiceKeepsEveryArchive(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Issue(dir, "bot", mtls.ClientUsage, mtls.RoleBot, 24*time.Hour, testNow); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Renew(dir, "bot", 24*time.Hour, testNow); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(dir, "archive", "bot-*", "cert.pem"))
	if err != nil || len(matches) != 2 {
		t.Fatalf("archive = %v, %v", matches, err)
	}
}

func TestRenewMissingLeafFails(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "Sylph CA", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Renew(dir, "bot", 24*time.Hour, testNow); err == nil {
		t.Fatal("renew of a missing leaf should fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "archive")); err == nil {
		t.Fatal("a failed renew must not create the archive")
	}
}
