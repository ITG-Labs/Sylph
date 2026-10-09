package mtls

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func mustKeyPEM(t *testing.T, leaf *Leaf) []byte {
	t.Helper()
	keyPEM, err := EncodeKeyPEM(leaf.Key)
	if err != nil {
		t.Fatal(err)
	}
	return keyPEM
}

type testEnv struct {
	ca *CA
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return &testEnv{ca: mustCA(t)}
}

func (e *testEnv) server(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	serverLeaf, err := e.ca.Issue("core.sylph.internal", ServerUsage, time.Now(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := NewServerTLSConfig(ServerConfig{
		CertPEM:     EncodeCertPEM(serverLeaf.Cert),
		KeyPEM:      mustKeyPEM(t, serverLeaf),
		ClientCAPEM: EncodeCertPEM(e.ca.Cert),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func (e *testEnv) client(t *testing.T, name string, usage LeafUsage) *http.Client {
	t.Helper()
	leaf, err := e.ca.Issue(name, usage, time.Now(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := NewClientTLSConfig(ClientConfig{
		CertPEM:    EncodeCertPEM(leaf.Cert),
		KeyPEM:     mustKeyPEM(t, leaf),
		CAPEM:      EncodeCertPEM(e.ca.Cert),
		ServerName: "core.sylph.internal",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
}

func TestTLSHandshake(t *testing.T) {
	env := newTestEnv(t)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 {
			t.Error("no client cert on server side")
		}
		if r.TLS.PeerCertificates[0].Subject.CommonName != "bot" {
			t.Errorf("client CN = %q, want bot", r.TLS.PeerCertificates[0].Subject.CommonName)
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := env.server(t, ok)
	client := env.client(t, "bot", ClientUsage)
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestTLSHandshakeWrongEKURejected(t *testing.T) {
	env := newTestEnv(t)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := env.server(t, ok)
	client := env.client(t, "evil", ServerUsage)
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("handshake with wrong EKU cert should fail")
	}
}

func TestNewServerTLSConfigRejectsBadCA(t *testing.T) {
	_, err := NewServerTLSConfig(ServerConfig{
		CertPEM:     []byte("bogus"),
		KeyPEM:      []byte("bogus"),
		ClientCAPEM: []byte("bogus"),
	})
	if err == nil {
		t.Fatal("bad PEM should fail")
	}
}

func TestNewClientTLSConfigRejectsBadCA(t *testing.T) {
	_, err := NewClientTLSConfig(ClientConfig{
		CertPEM: []byte("bogus"),
		KeyPEM:  []byte("bogus"),
		CAPEM:   []byte("bogus"),
	})
	if err == nil {
		t.Fatal("bad PEM should fail")
	}
}
