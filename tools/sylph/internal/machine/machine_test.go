package machine

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func TestDNS(t *testing.T) {
	local := []netip.Addr{netip.MustParseAddr("203.0.113.10")}
	answers := map[string][]netip.Addr{
		"ok.example.com":   {netip.MustParseAddr("203.0.113.10")},
		"nat.example.com":  {netip.MustParseAddr("198.51.100.7")},
		"v6.example.com":   {netip.MustParseAddr("203.0.113.10"), netip.MustParseAddr("2001:db8::1")},
		"only6.example.com": {netip.MustParseAddr("2001:db8::1")},
	}
	LookupIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
		if a, ok := answers[host]; ok {
			return a, nil
		}
		return nil, errors.New("no such host")
	}
	t.Cleanup(func() { LookupIP = func(ctx context.Context, host string) ([]netip.Addr, error) { return net.DefaultResolver.LookupNetIP(ctx, "ip", host) } })
	cases := map[string][]Level{
		"ok.example.com":      {OK},
		"nat.example.com":     {Warn},
		"v6.example.com":      {OK, Warn},
		"only6.example.com":   {Warn, Warn},
		"missing.example.com": {Warn},
	}
	for domain, want := range cases {
		got := DNS(context.Background(), domain, local)
		if len(got) != len(want) {
			t.Fatalf("%s: %+v", domain, got)
		}
		for i := range want {
			if got[i].Level != want[i] {
				t.Fatalf("%s[%d] = %+v, want level %d", domain, i, got[i], want[i])
			}
		}
	}
}

func TestTelegramGetMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/botgood/"):
			w.Write([]byte(`{"ok":true,"result":{"username":"sylph_bot"}}`))
		case strings.HasPrefix(r.URL.Path, "/botbad/"):
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"ok":false,"error_code":401}`))
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	old := TelegramAPI
	TelegramAPI = srv.URL
	defer func() { TelegramAPI = old }()
	if r := TelegramGetMe(context.Background(), "good"); r.Level != OK || !strings.Contains(r.Detail, "@sylph_bot") {
		t.Fatalf("good = %+v", r)
	}
	if r := TelegramGetMe(context.Background(), "bad"); r.Level != Fail {
		t.Fatalf("bad = %+v", r)
	}
	if r := TelegramGetMe(context.Background(), "flaky"); r.Level != Warn {
		t.Fatalf("flaky = %+v", r)
	}
	TelegramAPI = "http://127.0.0.1:1"
	r := TelegramGetMe(context.Background(), "123456:secret-token-value")
	if r.Level != Warn || strings.Contains(r.Detail, "secret-token-value") {
		t.Fatalf("offline = %+v", r)
	}
}

type testPKI struct {
	ca         *mtls.CA
	caPEM      []byte
	serverCert []byte
	serverKey  []byte
}

func newPKI(t *testing.T) testPKI {
	t.Helper()
	now := time.Now()
	ca, err := mtls.NewCA("Test CA", now)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := ca.Issue("core.sylph.internal", mtls.ServerUsage, "", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := mtls.EncodeKeyPEM(srv.Key)
	return testPKI{ca: ca, caPEM: mtls.EncodeCertPEM(ca.Cert), serverCert: mtls.EncodeCertPEM(srv.Cert), serverKey: key}
}

func (p testPKI) client(t *testing.T, role string) ClientCert {
	t.Helper()
	leaf, err := p.ca.Issue("probe-"+role, mtls.ClientUsage, role, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := mtls.EncodeKeyPEM(leaf.Key)
	return ClientCert{CA: p.caPEM, CertPEM: mtls.EncodeCertPEM(leaf.Cert), KeyPEM: key}
}

func startCore(t *testing.T, p testPKI, status int) string {
	t.Helper()
	cert, err := tlsPair(p.serverCert, p.serverKey)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	srv.TLS = serverTLS(cert, p.ca)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestProbeCore(t *testing.T) {
	p := newPKI(t)
	url := startCore(t, p, http.StatusNotFound)
	if r := ProbeCore(context.Background(), url, "core.sylph.internal", p.client(t, mtls.RoleNode)); r.Level != OK {
		t.Fatalf("good probe = %+v", r)
	}
	if r := ProbeCore(context.Background(), url, "other.name", p.client(t, mtls.RoleNode)); r.Level != Fail {
		t.Fatalf("wrong name = %+v", r)
	}
	other := newPKI(t)
	if r := ProbeCore(context.Background(), url, "core.sylph.internal", other.client(t, mtls.RoleNode)); r.Level != Fail {
		t.Fatalf("foreign CA = %+v", r)
	}
	forbidden := startCore(t, p, http.StatusForbidden)
	if r := ProbeCore(context.Background(), forbidden, "core.sylph.internal", p.client(t, mtls.RoleNode)); r.Level != Fail {
		t.Fatalf("revoked = %+v", r)
	}
	if r := ProbeCore(context.Background(), "https://127.0.0.1:1", "core.sylph.internal", p.client(t, mtls.RoleNode)); r.Level != Warn {
		t.Fatalf("down = %+v", r)
	}
}
