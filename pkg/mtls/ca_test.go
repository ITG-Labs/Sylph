package mtls

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestNewCA(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ca, err := NewCA("Sylph Test CA", now)
	if err != nil {
		t.Fatal(err)
	}
	if !ca.Cert.IsCA {
		t.Fatal("cert should be CA")
	}
	if !ca.Cert.BasicConstraintsValid {
		t.Fatal("basic constraints should be valid")
	}
	if ca.Cert.Subject.CommonName != "Sylph Test CA" {
		t.Fatalf("cn = %q", ca.Cert.Subject.CommonName)
	}
	if ca.Cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatal("missing CertSign key usage")
	}
	if ca.Cert.KeyUsage&x509.KeyUsageCRLSign == 0 {
		t.Fatal("missing CRLSign key usage")
	}
	if err := ca.Cert.CheckSignatureFrom(ca.Cert); err != nil {
		t.Fatalf("self-signature invalid: %v", err)
	}
	if !ca.Cert.NotBefore.Equal(now.Add(-time.Hour)) {
		t.Fatalf("notBefore = %v, want %v", ca.Cert.NotBefore, now.Add(-time.Hour))
	}
	if !ca.Cert.NotAfter.Equal(now.AddDate(10, 0, 0)) {
		t.Fatalf("notAfter = %v, want %v", ca.Cert.NotAfter, now.AddDate(10, 0, 0))
	}
}

func TestNewCASerialsDiffer(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ca1, err := NewCA("CA1", now)
	if err != nil {
		t.Fatal(err)
	}
	ca2, err := NewCA("CA2", now)
	if err != nil {
		t.Fatal(err)
	}
	if ca1.Cert.SerialNumber.Cmp(ca2.Cert.SerialNumber) == 0 {
		t.Fatal("serials should differ")
	}
}
