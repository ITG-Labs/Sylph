package api

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func mustKeyPEM(t *testing.T, leaf *mtls.Leaf) []byte {
	t.Helper()
	keyPEM, err := mtls.EncodeKeyPEM(leaf.Key)
	if err != nil {
		t.Fatal(err)
	}
	return keyPEM
}

func mTLSClient(t *testing.T, ca *mtls.CA, cn string) *http.Client {
	t.Helper()
	leaf, err := ca.Issue(cn, mtls.ClientUsage, time.Now(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
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

func newAPITestEnv(t *testing.T) (*httptest.Server, *http.Client, *service.Service) {
	t.Helper()
	pool := store.TestPoolWithSchema(t, "api_test")
	ctx := t.Context()
	if err := store.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := store.NewStore(pool)
	svc := service.NewService(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := NewServer(svc, testLogger(), mtls.NewRevocationList(nil))
	ts := httptest.NewUnstartedServer(srv.Handler())
	ca, err := mtls.NewCA("Sylph Test CA", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	serverLeaf, err := ca.Issue("core.sylph.internal", mtls.ServerUsage, time.Now(), 24*time.Hour)
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
	client := mTLSClient(t, ca, "node-1")
	return ts, client, svc
}

var _ = tls.VersionTLS12
