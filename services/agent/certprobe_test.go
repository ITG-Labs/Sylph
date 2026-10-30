package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func certFor(t *testing.T, name string, notAfter time.Time) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func serveCerts(t *testing.T, certs map[string]tls.Certificate) int {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{GetCertificate: func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if c, ok := certs[h.ServerName]; ok {
			return &c, nil
		}
		if c, ok := certs["*"]; ok {
			return &c, nil
		}
		return nil, errors.New("no certificate")
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = c.(*tls.Conn).Handshake()
				_ = c.Close()
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestProbeCertsReportsWhatClientsSee(t *testing.T) {
	expiry := time.Date(2027, 1, 28, 10, 0, 0, 0, time.UTC)
	port := serveCerts(t, map[string]tls.Certificate{
		"cdn.example.ru": certFor(t, "cdn.example.ru", expiry),
		"*":              certFor(t, "other.example.ru", expiry),
	})
	old := time.Date(2026, 1, 28, 10, 0, 0, 0, time.UTC)
	stale := serveCerts(t, map[string]tls.Certificate{"old.example.ru": certFor(t, "old.example.ru", old)})
	got := probeCerts(context.Background(), map[string]int{"cdn.example.ru": port, "wrong.example.ru": port, "down.example.ru": 1, "old.example.ru": stale})
	if st := got["old.example.ru"]; st.NotAfter == nil || !st.NotAfter.Equal(old) || !strings.Contains(st.Error, "expired") {
		t.Fatalf("expired = %+v", st)
	}
	if st := got["cdn.example.ru"]; st.NotAfter == nil || !st.NotAfter.Equal(expiry) || st.Error != "" {
		t.Fatalf("issued = %+v", st)
	}
	if st := got["wrong.example.ru"]; st.NotAfter != nil || !strings.Contains(st.Error, "wrong.example.ru") {
		t.Fatalf("foreign certificate = %+v", st)
	}
	if st := got["down.example.ru"]; st.NotAfter != nil || st.Error == "" {
		t.Fatalf("unreachable = %+v", st)
	}
	if len(got) != 4 {
		t.Fatalf("statuses = %+v", got)
	}
}

func TestHeartbeatReportsCertificates(t *testing.T) {
	expiry := time.Date(2027, 1, 28, 10, 0, 0, 0, time.UTC)
	port := serveCerts(t, map[string]tls.Certificate{"cdn.example.ru": certFor(t, "cdn.example.ru", expiry)})
	cdn := cdnInbound("cdn", "cdn.example.ru", "/api")
	cdn.Port = port
	inbounds := []Inbound{testInbound("alpha"), cdn}
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, syncInbounds: inbounds}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	preapply(t, loop, inbounds)
	tick(t, loop)
	hb, _ := lastHeartbeat(fc)
	st, _ := hb["tls_status"].(map[string]any)
	cert, _ := st["cdn.example.ru"].(map[string]any)
	if len(st) != 1 || cert["not_after"] != expiry.Format(time.RFC3339) {
		t.Fatalf("tls_status = %v", hb["tls_status"])
	}
	if !fc.xhttp {
		t.Fatal("agent did not tell the core it knows xhttp")
	}
}

func TestHeartbeatWithoutTLSInboundsHasNoCertificates(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, syncInbounds: testInbounds("alpha")}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	preapply(t, loop, testInbounds("alpha"))
	tick(t, loop)
	if hb, _ := lastHeartbeat(fc); hb["tls_status"] != nil {
		t.Fatalf("tls_status = %v", hb["tls_status"])
	}
}

func TestCaddyGetsTheConfiguredACMEServer(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.cfg.ACMECA, r.cfg.ACMECARoot = "https://pebble:14000/dir", "/pebble/root.pem"
	r.node.Apply(context.Background(), []Inbound{cdnInbound("cdn", "cdn.example.ru", "/api")})
	r.caddy.mu.Lock()
	loaded := decodeConfig(t, r.caddy.current)
	r.caddy.mu.Unlock()
	issuer := dig(loaded, "apps", "tls", "automation", "policies", 0, "issuers", 0)
	if dig(issuer, "ca") != "https://pebble:14000/dir" || dig(issuer, "trusted_roots_pem_files", 0) != "/pebble/root.pem" {
		t.Fatalf("caddy issuer = %v", issuer)
	}
}

func TestLoadConfigACMEServer(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SYLPH_ACME_CA", "https://pebble:14000/dir")
	t.Setenv("SYLPH_ACME_CA_ROOT", "/pebble/root.pem")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ACMECA != "https://pebble:14000/dir" || cfg.ACMECARoot != "/pebble/root.pem" {
		t.Fatalf("acme = %q %q", cfg.ACMECA, cfg.ACMECARoot)
	}
}
