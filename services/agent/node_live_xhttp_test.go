package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/agent/internal/xraygrpc"
)

const (
	liveDecoy      = "127.0.0.1:28081"
	liveCDNPacket  = "client:10803"
	liveCDNOne     = "client:10804"
	liveCDNUp      = "client:10805"
	liveRealityXH  = "client:10806"
	liveCDNPath    = "/api/v2/catalog/sync.json"
	liveCDNDomain  = "cdn.test"
	liveDecoySite  = "http://dest.test:28081"
	liveRXServerNm = "rx.test"
)

func liveXHTTPInbounds() []Inbound {
	extra := json.RawMessage(ownerExtraJSON)
	rx := Inbound{
		Tag: "rx", Port: 443, Protocol: "vless", Transport: "xhttp", Security: "reality",
		Reality: &Reality{Dest: "dest.test:28443", ServerNames: []string{liveRXServerNm}, ShortIDs: []string{"a1b2c3d4e5f60718"}, Fingerprint: "chrome", PrivateKey: liveAlphaKey},
		XHTTP:   &XHTTP{Path: "/sync", Mode: "auto", ClientMode: "auto"},
	}
	cdn := Inbound{
		Tag: "cdn", Port: 443, Protocol: "vless", Transport: "xhttp", Security: "tls",
		XHTTP: &XHTTP{Path: liveCDNPath, Mode: "auto", ClientMode: "packet-up", Extra: extra},
		TLS:   &TLS{Domain: liveCDNDomain, Decoy: liveDecoySite},
	}
	feed := Inbound{
		Tag: "feed", Port: 443, Protocol: "vless", Transport: "xhttp", Security: "tls",
		XHTTP: &XHTTP{Path: "/feed", Mode: "auto", ClientMode: "auto"},
		TLS:   &TLS{Domain: liveCDNDomain, Decoy: liveDecoySite},
	}
	return append(liveNodeInbounds(), rx, cdn, feed)
}

func serveDecoy(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", liveDecoy)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprintf(w, "decoy %s %s %s", r.Host, r.Method, r.URL.Path)
		}),
		ErrorLog: log.New(io.Discard, "", 0),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func within(t *testing.T, what string, d time.Duration, f func() error) {
	t.Helper()
	var err error
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		if err = f(); err == nil {
			return
		}
	}
	t.Fatalf("%s: %v", what, err)
}

func probeSite(method, path string) (*http.Response, string, error) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", "127.0.0.1:443")
		},
		TLSClientConfig: &tls.Config{ServerName: liveCDNDomain, InsecureSkipVerify: true},
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequest(method, "https://"+liveCDNDomain+path, strings.NewReader("probe"))
	if err != nil {
		return nil, "", err
	}
	resp, err := (&http.Client{Transport: tr, Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return resp, string(body), err
}

func TestNodeLiveXHTTP(t *testing.T) {
	if os.Getenv("SYLPH_TEST_NODE") == "" {
		t.Skip("SYLPH_TEST_NODE not set, run make test-node")
	}
	ctx := context.Background()
	cfg := &Config{
		XrayAPI:       defaultXrayAPI,
		XrayConfig:    defaultXrayConfig,
		CaddyAdmin:    defaultCaddyAdmin,
		NodeStatePath: filepath.Join(t.TempDir(), "node.json"),
		ACMECA:        os.Getenv("SYLPH_ACME_CA"),
		ACMECARoot:    os.Getenv("SYLPH_ACME_CA_ROOT"),
	}
	x, err := xraygrpc.Dial(cfg.XrayAPI, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	if !WaitForXray(ctx, x, 30*time.Second, 200*time.Millisecond, discardLogger()) {
		t.Fatal("xray did not start")
	}
	serveMaskSite(t)
	serveEcho(t)
	serveDecoy(t)
	node := NewNodeConfig(cfg, x, NewCaddyAPI(cfg.CaddyAdmin, 5*time.Second), discardLogger())
	node.buildXray = allowLoopback(cfg.XrayAPI)
	node.restartWait = 15 * time.Second
	engine := NewEngine(cfg, nil, x, discardLogger())
	engine.setDesired(users(liveUser))
	in := liveXHTTPInbounds()

	applied := node.Apply(ctx, in)
	if !reflect.DeepEqual(applied, in) || node.ConfigError() != nil {
		t.Fatalf("applied = %+v, config_error = %v", applied, node.ConfigError())
	}
	if err := engine.Reconcile(ctx, applied); err != nil {
		t.Fatal(err)
	}

	t.Run("the node gets a certificate for its domain over 443", func(t *testing.T) {
		within(t, "certificate from the ACME server", 60*time.Second, func() error {
			st := probeCerts(ctx, tlsDomains(applied))[liveCDNDomain]
			if st.NotAfter == nil {
				return fmt.Errorf("status %+v", st)
			}
			if time.Until(*st.NotAfter) < 24*time.Hour {
				return fmt.Errorf("certificate expires too soon: %v", st.NotAfter)
			}
			return nil
		})
	})

	t.Run("xhttp over tls carries traffic in every mode", func(t *testing.T) {
		for _, c := range []struct{ name, addr string }{{"packet-up", liveCDNPacket}, {"stream-one", liveCDNOne}, {"stream-up", liveCDNUp}} {
			within(t, c.name, 20*time.Second, func() error { return echoVia(c.addr, "hello-"+c.name) })
		}
		within(t, "traffic counted on the cdn inbound", 10*time.Second, func() error {
			stats, err := x.QueryStats(ctx, "user>>>"+userEmail(liveUser, "cdn")+">>>")
			if err != nil {
				return err
			}
			for name, v := range stats {
				if strings.HasSuffix(name, ">>>uplink") && v > 0 {
					return nil
				}
			}
			return fmt.Errorf("no uplink on cdn: %v", stats)
		})
	})

	t.Run("xray sees the address caddy got, not its own", func(t *testing.T) {
		within(t, "online ip of the cdn user", 10*time.Second, func() error {
			online, err := x.OnlineIPs(ctx)
			if err != nil {
				return err
			}
			ips := online[userEmail(liveUser, "cdn")]
			if len(ips) == 0 {
				return fmt.Errorf("no online ips: %v", online)
			}
			for ip := range ips {
				if ip == "127.0.0.1" {
					return fmt.Errorf("xray saw caddy, not the client: %v", ips)
				}
			}
			return nil
		})
	})

	t.Run("xhttp over reality goes through caddy by sni", func(t *testing.T) {
		within(t, "reality xhttp", 20*time.Second, func() error { return echoVia(liveRealityXH, "hello-rx") })
	})

	t.Run("reality over tcp still works next to them", func(t *testing.T) {
		within(t, "alpha", 10*time.Second, func() error { return echoVia(liveClientA, "hello-alpha") })
	})

	t.Run("scanners get the decoy site without xray traces", func(t *testing.T) {
		for _, c := range []struct{ method, path, want string }{
			{http.MethodGet, "/", "decoy dest.test:28081 GET /"},
			{http.MethodGet, "/admin", "decoy dest.test:28081 GET /admin"},
			{http.MethodGet, liveCDNPath, "decoy dest.test:28081 GET /"},
			{http.MethodPost, liveCDNPath, "decoy dest.test:28081 GET /"},
			{http.MethodGet, liveCDNPath + "/", "decoy dest.test:28081 GET " + liveCDNPath + "/"},
			{http.MethodGet, "/feed/x", "decoy dest.test:28081 GET /"},
		} {
			resp, body, err := probeSite(c.method, c.path)
			if err != nil {
				t.Fatalf("%s %s: %v", c.method, c.path, err)
			}
			if resp.StatusCode != http.StatusOK || body != c.want {
				t.Fatalf("%s %s: %d %q, want %q", c.method, c.path, resp.StatusCode, body, c.want)
			}
			for _, h := range append(append([]string{}, xrayHeaders...), edgeHeader) {
				if resp.Header.Get(h) != "" {
					t.Fatalf("%s %s: response carries %s: %v", c.method, c.path, h, resp.Header)
				}
			}
		}
	})

	t.Run("unknown sni still gets the reality mask site", func(t *testing.T) {
		name, err := peerName("127.0.0.1:443", "scanner.example", false)
		if err != nil || name != "dest.test" {
			t.Fatalf("scanner saw %q (err %v), want the mask site", name, err)
		}
	})

	t.Run("same inbounds leave xray and caddy alone", func(t *testing.T) {
		before := liveUptime(t, x)
		node.Apply(ctx, in)
		if after := liveUptime(t, x); after < before {
			t.Fatalf("xray restarted: uptime %v -> %v", before, after)
		}
		if node.ConfigError() != nil {
			t.Fatalf("config_error = %v", *node.ConfigError())
		}
	})
}
