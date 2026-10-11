package mtls

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsRevoked(t *testing.T) {
	ca := mustCA(t)
	revoked, err := ca.Issue("node-3", ClientUsage, RoleNode, testNow, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := ca.Issue("node-4", ClientUsage, RoleNode, testNow, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	list := NewRevocationList([]string{revoked.Cert.SerialNumber.String()})
	if !list.IsRevoked(revoked.Cert) {
		t.Fatal("revoked cert should be revoked")
	}
	if list.IsRevoked(valid.Cert) {
		t.Fatal("valid cert should not be revoked")
	}
	if list.IsRevoked(nil) {
		t.Fatal("nil cert should not panic")
	}
}

func TestRevocationMiddlewareRevoked(t *testing.T) {
	env := newTestEnv(t)
	revoked, err := env.ca.Issue("node-3", ClientUsage, RoleNode, time.Now(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	list := NewRevocationList([]string{revoked.Cert.SerialNumber.String()})
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := env.server(t, list.Middleware(ok))
	cfg, err := NewClientTLSConfig(ClientConfig{
		CertPEM:    EncodeCertPEM(revoked.Cert),
		KeyPEM:     mustKeyPEM(t, revoked),
		CAPEM:      EncodeCertPEM(env.ca.Cert),
		ServerName: "core.sylph.internal",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRevocationMiddlewareValid(t *testing.T) {
	env := newTestEnv(t)
	list := NewRevocationList(nil)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := env.server(t, list.Middleware(ok))
	client := env.client(t, "bot", ClientUsage, RoleBot)
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRevocationMiddlewareNoTLS(t *testing.T) {
	list := NewRevocationList(nil)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(list.Middleware(ok))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
