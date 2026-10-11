package mtls

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
	"time"
)

func TestIssueClientCertCarriesRole(t *testing.T) {
	ca := mustCA(t)
	leaf, err := ca.Issue("bot-2027", ClientUsage, RoleBot, testNow, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	role, ok := RoleOf(leaf.Cert)
	if !ok || role != RoleBot {
		t.Fatalf("role = %q, %v; want bot", role, ok)
	}
	if leaf.Cert.Subject.CommonName != "bot-2027" {
		t.Fatalf("cn = %q", leaf.Cert.Subject.CommonName)
	}
}

func TestIssueClientCertRequiresKnownRole(t *testing.T) {
	ca := mustCA(t)
	if _, err := ca.Issue("bot", ClientUsage, "", testNow, time.Hour); err == nil {
		t.Fatal("client cert without role must fail")
	}
	if _, err := ca.Issue("bot", ClientUsage, "admin", testNow, time.Hour); err == nil {
		t.Fatal("client cert with unknown role must fail")
	}
}

func TestIssueServerCertRejectsRole(t *testing.T) {
	ca := mustCA(t)
	if _, err := ca.Issue("core.sylph.internal", ServerUsage, RolePanel, testNow, time.Hour); err == nil {
		t.Fatal("server cert with role must fail")
	}
	leaf, err := ca.Issue("core.sylph.internal", ServerUsage, "", testNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := RoleOf(leaf.Cert); ok {
		t.Fatal("server cert must not carry a role")
	}
}

func TestRoleOf(t *testing.T) {
	cases := []struct {
		name string
		ou   []string
		want string
		ok   bool
	}{
		{"none", nil, "", false},
		{"single known", []string{"node"}, "node", true},
		{"single unknown", []string{"admin"}, "", false},
		{"two roles", []string{"bot", "panel"}, "", false},
		{"empty string", []string{""}, "", false},
	}
	for _, c := range cases {
		cert := &x509.Certificate{Subject: pkix.Name{CommonName: "x", OrganizationalUnit: c.ou}}
		got, ok := RoleOf(cert)
		if got != c.want || ok != c.ok {
			t.Fatalf("%s: RoleOf = %q, %v; want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
	if _, ok := RoleOf(nil); ok {
		t.Fatal("nil cert must have no role")
	}
}
