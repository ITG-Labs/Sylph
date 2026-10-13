package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type apiEnv struct {
	ts   *httptest.Server
	ca   *mtls.CA
	svc  *service.Service
	logs *lockedBuffer
	pool *pgxpool.Pool
}

func mustKeyPEM(t *testing.T, leaf *mtls.Leaf) []byte {
	t.Helper()
	keyPEM, err := mtls.EncodeKeyPEM(leaf.Key)
	if err != nil {
		t.Fatal(err)
	}
	return keyPEM
}

func leafClient(t *testing.T, ca *mtls.CA, leaf *mtls.Leaf) *http.Client {
	t.Helper()
	cfg, err := mtls.NewClientTLSConfig(mtls.ClientConfig{
		CertPEM:    mtls.EncodeCertPEM(leaf.Cert),
		KeyPEM:     mustKeyPEM(t, leaf),
		CAPEM:      mtls.EncodeCertPEM(ca.Cert),
		ServerName: "core.sylph.internal",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
}

func rawLeaf(t *testing.T, ca *mtls.CA, cn string, ou []string) *mtls.Leaf {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn, OrganizationalUnit: ou},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &mtls.Leaf{Cert: cert, Key: key}
}

func newAPIEnv(t *testing.T) *apiEnv {
	t.Helper()
	ca, err := mtls.NewCA("Sylph Test CA", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return newAPIEnvWith(t, ca, mtls.NewRevocationList(nil))
}

func newAPIEnvWith(t *testing.T, ca *mtls.CA, revoked *mtls.RevocationList) *apiEnv {
	t.Helper()
	pool := store.TestPoolWithSchema(t, "api_test")
	ctx := t.Context()
	if err := store.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	svc := service.NewService(store.NewStore(pool), logger)
	srv := NewServer(svc, logger, revoked)
	ts := httptest.NewUnstartedServer(srv.Handler())
	serverLeaf, err := ca.Issue("core.sylph.internal", mtls.ServerUsage, "", time.Now(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mtls.NewServerTLSConfig(mtls.ServerConfig{
		CertPEM:     mtls.EncodeCertPEM(serverLeaf.Cert),
		KeyPEM:      mustKeyPEM(t, serverLeaf),
		ClientCAPEM: mtls.EncodeCertPEM(ca.Cert),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts.TLS = cfg
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return &apiEnv{ts: ts, ca: ca, svc: svc, logs: logs, pool: pool}
}

func (e *apiEnv) client(t *testing.T, cn string, role string) *http.Client {
	t.Helper()
	leaf, err := e.ca.Issue(cn, mtls.ClientUsage, role, time.Now(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return leafClient(t, e.ca, leaf)
}

func (e *apiEnv) clientWithOU(t *testing.T, cn string, ou []string) *http.Client {
	t.Helper()
	return leafClient(t, e.ca, rawLeaf(t, e.ca, cn, ou))
}

func newAPITestEnv(t *testing.T, role string) (*httptest.Server, *http.Client, *service.Service) {
	t.Helper()
	e := newAPIEnv(t)
	cn := role
	if role == mtls.RoleNode {
		cn = "node-1"
	}
	return e.ts, e.client(t, cn, role), e.svc
}
