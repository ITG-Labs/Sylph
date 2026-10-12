package subs

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/apiclient"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/pkg/mtls"
)

const liveTargetBody = "sylph-e2e-ok"

type liveEnv struct {
	panel  *apiclient.Client
	subs   string
	node   string
	target string
}

type liveUser struct {
	ID       int64  `json:"id"`
	SubToken string `json:"sub_token"`
}

func TestSubsLive(t *testing.T) {
	if os.Getenv("SYLPH_TEST_SUBS") == "" {
		t.Skip("SYLPH_TEST_SUBS not set, run make test-subs")
	}
	env := &liveEnv{
		panel:  livePanel(t),
		subs:   os.Getenv("SYLPH_E2E_SUBS"),
		node:   os.Getenv("SYLPH_E2E_NODE"),
		target: "http://" + os.Getenv("SYLPH_E2E_TARGET") + "/",
	}
	serveLiveMaskSite(t)
	serveLiveTarget(t)
	ctx := context.Background()
	liveEventually(t, 60*time.Second, "core is up", func() error {
		_, err := env.panel.Health(ctx)
		return err
	})

	var node struct {
		ID int64 `json:"id"`
	}
	env.call(t, http.MethodPost, "/v1/admin/nodes", map[string]any{"cert_cn": "node-1", "name": "E2E", "address": env.node}, &node)
	env.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", node.ID), map[string]any{
		"tag": "reality", "port": 443, "reality": map[string]any{"dest": "mask.test:443", "server_names": []string{"mask.test"}},
	}, nil)
	var plan, devicePlan struct {
		ID int64 `json:"id"`
	}
	env.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "Месяц", "price_rub": 199, "period_days": 30}, &plan)
	env.call(t, http.MethodPost, "/v1/admin/plans", map[string]any{"name": "Одно устройство", "price_rub": 99, "period_days": 30, "device_limit": 1}, &devicePlan)
	clashUser := env.user(t, 1001, plan.ID)
	linksUser := env.user(t, 1002, plan.ID)
	expiredUser := env.user(t, 1003, 0)
	deviceUser := env.user(t, 1004, devicePlan.ID)

	liveEventually(t, 60*time.Second, "node applied its inbound and took the users", func() error {
		var nodes []struct {
			ID          int64   `json:"id"`
			ActiveUsers *int    `json:"active_users"`
			ConfigError *string `json:"config_error"`
		}
		if err := env.panel.Do(ctx, http.MethodGet, "/v1/admin/nodes", nil, &nodes); err != nil {
			return err
		}
		for _, n := range nodes {
			if n.ID == node.ID && n.ConfigError == nil && n.ActiveUsers != nil && *n.ActiveUsers == 3 {
				return nil
			}
		}
		return fmt.Errorf("nodes = %+v", nodes)
	})

	t.Run("health and unknown tokens", func(t *testing.T) {
		if code, _, _ := env.fetch(t, "/health", nil); code != http.StatusOK {
			t.Fatalf("health = %d", code)
		}
		for _, path := range []string{"/" + strings.Repeat("0", 32), "/not-a-token"} {
			if code, _, _ := env.fetch(t, path, nil); code != http.StatusNotFound {
				t.Fatalf("%s = %d", path, code)
			}
		}
	})

	t.Run("statuses become stubs with a reason", func(t *testing.T) {
		happ := map[string]string{"User-Agent": "Happ/3.6.1"}
		if got := liveStub(t, env, expiredUser, happ); got != StubExpired {
			t.Fatalf("expired user got %q", got)
		}
		if got := liveStub(t, env, deviceUser, happ); got != StubHWIDRequired {
			t.Fatalf("device plan without hwid got %q", got)
		}
		first := map[string]string{"User-Agent": "Happ/3.6.1", "x-hwid": "e2e-device-0001", "x-device-os": "iOS", "x-ver-os": "18.3", "x-device-model": "iPhone"}
		if links := liveLinks(t, env, deviceUser, first); len(links) != 1 {
			t.Fatalf("first device got %q", links)
		}
		second := map[string]string{"User-Agent": "Happ/3.6.1", "x-hwid": "e2e-device-0002"}
		if got := liveStub(t, env, deviceUser, second); got != StubDeviceLimit {
			t.Fatalf("second device got %q", got)
		}
		if links := liveLinks(t, env, deviceUser, first); len(links) != 1 {
			t.Fatalf("known device lost access: %q", links)
		}
	})

	t.Run("sing-box accepts both profiles", func(t *testing.T) {
		code, _, body := env.fetch(t, "/"+clashUser.SubToken, map[string]string{"User-Agent": "SFA/1.14.3"})
		if code != http.StatusOK || !bytes.Contains(body, []byte(StubSingBox)) {
			t.Fatalf("sing-box got %d %s", code, body)
		}
		singBoxCheck(t, body)
		var view View
		env.call(t, http.MethodPost, "/v1/subscription", Request{SubToken: clashUser.SubToken, UserAgent: "SFA/1.14.3"}, &view)
		full, err := RenderSingBox(view.Connections)
		if err != nil {
			t.Fatal(err)
		}
		singBoxCheck(t, full)
	})

	t.Run("mihomo runs our YAML profile through the node", func(t *testing.T) {
		code, hdr, body := env.fetch(t, "/"+clashUser.SubToken, map[string]string{"User-Agent": "clash-verge/v2.4.2"})
		if code != http.StatusOK || hdr.Get("Content-Type") != "text/yaml; charset=utf-8" {
			t.Fatalf("clash got %d %s", code, hdr.Get("Content-Type"))
		}
		proxy := startMihomo(t, body, 7890)
		liveEventually(t, 30*time.Second, "target reached through mihomo YAML", func() error { return env.probe(proxy) })
		env.waitTraffic(t, clashUser)
	})

	t.Run("mihomo reads our base64 subscription through the node", func(t *testing.T) {
		cfg := fmt.Sprintf(`mixed-port: 7891
mode: rule
log-level: info
proxy-providers:
  subs:
    type: http
    url: %s/%s?format=base64
    interval: 3600
proxy-groups:
  - name: e2e
    type: select
    use: [subs]
rules:
  - MATCH,e2e
`, env.subs, linksUser.SubToken)
		proxy := startMihomo(t, []byte(cfg), 7891)
		liveEventually(t, 30*time.Second, "target reached through mihomo base64", func() error { return env.probe(proxy) })
		env.waitTraffic(t, linksUser)
	})
}

func (e *liveEnv) waitTraffic(t *testing.T, u liveUser) {
	t.Helper()
	download := regexp.MustCompile(`download=(\d+);`)
	liveEventually(t, 60*time.Second, "user traffic came back in the subscription header", func() error {
		code, hdr, _, err := e.tryFetch("/"+u.SubToken, map[string]string{"User-Agent": "Happ/3.6.1"})
		if err != nil {
			return err
		}
		m := download.FindStringSubmatch(hdr.Get("Subscription-Userinfo"))
		if code != http.StatusOK || m == nil {
			return fmt.Errorf("subs = %d, userinfo = %q", code, hdr.Get("Subscription-Userinfo"))
		}
		if n, _ := strconv.ParseInt(m[1], 10, 64); n == 0 {
			return errors.New("no traffic counted yet")
		}
		return nil
	})
}

func livePanel(t *testing.T) *apiclient.Client {
	t.Helper()
	read := func(key string) []byte {
		b, err := os.ReadFile(os.Getenv(key))
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		return b
	}
	tlsCfg, err := mtls.NewClientTLSConfig(mtls.ClientConfig{
		CertPEM: read("SYLPH_E2E_PANEL_CERT"), KeyPEM: read("SYLPH_E2E_PANEL_KEY"), CAPEM: read("SYLPH_E2E_CA"), ServerName: "core.sylph.internal",
	})
	if err != nil {
		t.Fatal(err)
	}
	api, err := apiclient.New("e2e", os.Getenv("SYLPH_E2E_CORE"), tlsCfg, logging.NewWithWriter("e2e", "error", io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func (e *liveEnv) call(t *testing.T, method, path string, in, out any) {
	t.Helper()
	if err := e.panel.Do(context.Background(), method, path, in, out); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
}

func (e *liveEnv) user(t *testing.T, telegramID int64, planID int64) liveUser {
	t.Helper()
	var u liveUser
	e.call(t, http.MethodPost, "/v1/users", map[string]any{"telegram_id": telegramID}, &u)
	if planID != 0 {
		e.call(t, http.MethodPost, "/v1/payments", map[string]any{"payment_id": fmt.Sprintf("e2e-%d", telegramID), "user_id": u.ID, "plan_id": planID, "amount_rub": 199}, nil)
	}
	return u
}

func (e *liveEnv) fetch(t *testing.T, path string, header map[string]string) (int, http.Header, []byte) {
	t.Helper()
	code, hdr, body, err := e.tryFetch(path, header)
	if err != nil {
		t.Fatal(err)
	}
	return code, hdr, body
}

func (e *liveEnv) tryFetch(path string, header map[string]string) (int, http.Header, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, e.subs+path, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body, err
}

func (e *liveEnv) probe(proxy *url.URL) error {
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
	resp, err := c.Get(e.target)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != liveTargetBody {
		return fmt.Errorf("target answered %d %q", resp.StatusCode, body)
	}
	return nil
}

func liveLinks(t *testing.T, e *liveEnv, u liveUser, header map[string]string) []string {
	t.Helper()
	code, _, body := e.fetch(t, "/"+u.SubToken, header)
	if code != http.StatusOK {
		t.Fatalf("subs = %d %s", code, body)
	}
	raw, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		t.Fatalf("not base64: %s", body)
	}
	return strings.Split(string(raw), "\n")
}

func liveStub(t *testing.T, e *liveEnv, u liveUser, header map[string]string) string {
	t.Helper()
	links := liveLinks(t, e, u, header)
	if len(links) != 1 || !strings.Contains(links[0], "@127.0.0.1:1?") {
		return strings.Join(links, "\n")
	}
	name, err := url.PathUnescape(links[0][strings.Index(links[0], "#")+1:])
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func singBoxCheck(t *testing.T, profile []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, profile, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sing-box", "check", "-c", path).CombinedOutput(); err != nil {
		t.Fatalf("sing-box check: %v\n%s\n%s", err, out, profile)
	}
}

func startMihomo(t *testing.T, config []byte, port int) *url.URL {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, config, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := exec.Command("/mihomo", "-d", dir, "-f", path)
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("mihomo log:\n%s", out.String())
		}
	})
	return &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
}

func serveLiveMaskSite(t *testing.T) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mask.test"}, DNSNames: []string{"mask.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", ":443", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "mask") }),
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func serveLiveTarget(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", ":80")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, liveTargetBody) }),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func liveEventually(t *testing.T, timeout time.Duration, what string, f func() error) {
	t.Helper()
	var err error
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		if err = f(); err == nil {
			return
		}
	}
	t.Fatalf("%s: %v", what, err)
}
