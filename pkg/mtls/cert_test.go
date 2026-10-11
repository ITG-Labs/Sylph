package mtls

import (
	"crypto/x509"
	"testing"
	"time"
)

var testNow = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func mustCA(t *testing.T) *CA {
	t.Helper()
	ca, err := NewCA("Sylph Test CA", testNow)
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func hasEKU(cert *x509.Certificate, want x509.ExtKeyUsage) bool {
	for _, eku := range cert.ExtKeyUsage {
		if eku == want {
			return true
		}
	}
	return false
}

func TestIssueClientCert(t *testing.T) {
	ca := mustCA(t)
	leaf, err := ca.Issue("bot", ClientUsage, RoleBot, testNow, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Cert.Subject.CommonName != "bot" {
		t.Fatalf("cn = %q, want bot", leaf.Cert.Subject.CommonName)
	}
	if !hasEKU(leaf.Cert, x509.ExtKeyUsageClientAuth) {
		t.Fatal("client cert must have ClientAuth EKU")
	}
	if hasEKU(leaf.Cert, x509.ExtKeyUsageServerAuth) {
		t.Fatal("client cert must not have ServerAuth EKU")
	}
	if len(leaf.Cert.DNSNames) != 0 {
		t.Fatalf("client cert DNSNames = %v, want none", leaf.Cert.DNSNames)
	}
	if err := leaf.Cert.CheckSignatureFrom(ca.Cert); err != nil {
		t.Fatalf("signature by CA invalid: %v", err)
	}
	if !leaf.Cert.NotAfter.Equal(testNow.Add(24 * time.Hour)) {
		t.Fatalf("notAfter = %v, want %v", leaf.Cert.NotAfter, testNow.Add(24*time.Hour))
	}
}

func TestIssueServerCert(t *testing.T) {
	ca := mustCA(t)
	leaf, err := ca.Issue("core.sylph.internal", ServerUsage, "", testNow, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEKU(leaf.Cert, x509.ExtKeyUsageServerAuth) {
		t.Fatal("server cert must have ServerAuth EKU")
	}
	if len(leaf.Cert.DNSNames) != 1 || leaf.Cert.DNSNames[0] != "core.sylph.internal" {
		t.Fatalf("DNSNames = %v, want [core.sylph.internal]", leaf.Cert.DNSNames)
	}
}

func TestPEMRoundTrip(t *testing.T) {
	ca := mustCA(t)
	leaf, err := ca.Issue("bot", ClientUsage, RoleBot, testNow, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := EncodeCertPEM(leaf.Cert)
	parsedCert, err := ParseCertPEM(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if parsedCert.SerialNumber.Cmp(leaf.Cert.SerialNumber) != 0 {
		t.Fatal("serial mismatch after PEM round trip")
	}
	keyPEM, err := EncodeKeyPEM(leaf.Key)
	if err != nil {
		t.Fatal(err)
	}
	parsedKey, err := ParseKeyPEM(keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if !parsedKey.Equal(leaf.Key) {
		t.Fatal("key mismatch after PEM round trip")
	}
}
