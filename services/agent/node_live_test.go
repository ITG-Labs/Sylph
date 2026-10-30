package agent

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/big"
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
	liveAlphaKey = "8F0ET9EsN7sZiyXEsjQ2FnD1ew8BagsJ-BMWKb_s1nk"
	liveBetaKey  = "MDHqaJPhoMxwOUVfWKd2y8WK8XFd8mmQr0LHNlU2ZVE"
	liveUser     = "7b350ac3-6336-4365-afc6-d1b3192042f2"
	liveEcho     = "127.0.0.1:28080"
	liveDest     = "127.0.0.1:28443"
	liveClientA  = "client:10801"
	liveClientB  = "client:10802"
)

func liveNodeInbounds() []Inbound {
	in := func(tag, sni, key string) Inbound {
		return Inbound{
			Tag: tag, Port: 443, Protocol: "vless", Transport: "tcp", Security: "reality", Flow: "xtls-rprx-vision",
			Reality: &Reality{Dest: "dest.test:28443", ServerNames: []string{sni}, ShortIDs: []string{"a1b2c3d4e5f60718"}, Fingerprint: "chrome", PrivateKey: key},
		}
	}
	return []Inbound{in("beta", "beta.test", liveBetaKey), in("alpha", "alpha.test", liveAlphaKey)}
}

func allowLoopback(api string) func(XrayInput) ([]byte, error) {
	return func(in XrayInput) ([]byte, error) {
		in.APIListen = api
		data, err := BuildXrayRouted(in)
		if err != nil {
			return nil, err
		}
		var cfg map[string]any
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		cfg["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{
			"finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.1/32"}}},
		}
		return marshalConfig(cfg)
	}
}

func serveMaskSite(t *testing.T) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "dest.test"}, DNSNames: []string{"dest.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", liveDest, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler:  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "mask") }),
		ErrorLog: log.New(io.Discard, "", 0),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func serveEcho(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", liveEcho)
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
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
}

func echoOver(c net.Conn, msg string) error {
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		return err
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		return err
	}
	if string(buf) != msg {
		return errors.New("echo mismatch")
	}
	return nil
}

func echoVia(addr, msg string) error {
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return echoOver(c, msg)
}

func eventually(t *testing.T, what string, f func() error) {
	t.Helper()
	var err error
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if err = f(); err == nil {
			return
		}
	}
	t.Fatalf("%s: %v", what, err)
}

func peerName(addr, sni string, proxyHeader bool) (string, error) {
	raw, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return "", err
	}
	defer func() { _ = raw.Close() }()
	_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
	if proxyHeader {
		h := append([]byte("\r\n\r\n\x00\r\nQUIT\n"), 0x21, 0x11, 0, 12)
		h = append(h, 203, 0, 113, 7, 127, 0, 0, 1)
		h = binary.BigEndian.AppendUint16(h, 40000)
		h = binary.BigEndian.AppendUint16(h, 443)
		if _, err := raw.Write(h); err != nil {
			return "", err
		}
	}
	tc := tls.Client(raw, &tls.Config{ServerName: sni, InsecureSkipVerify: true})
	if err := tc.Handshake(); err != nil {
		return "", err
	}
	return tc.ConnectionState().PeerCertificates[0].Subject.CommonName, nil
}

func liveUptime(t *testing.T, x XrayClient) time.Duration {
	t.Helper()
	up, err := x.Uptime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return up
}

func TestNodeLive(t *testing.T) {
	if os.Getenv("SYLPH_TEST_NODE") == "" {
		t.Skip("SYLPH_TEST_NODE not set, run make test-node")
	}
	ctx := context.Background()
	cfg := &Config{
		XrayAPI:       defaultXrayAPI,
		XrayConfig:    defaultXrayConfig,
		CaddyAdmin:    defaultCaddyAdmin,
		NodeStatePath: filepath.Join(t.TempDir(), "node.json"),
	}
	x, err := xraygrpc.Dial(cfg.XrayAPI, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	if !WaitForXray(ctx, x, 30*time.Second, 200*time.Millisecond, discardLogger()) {
		t.Fatal("xray did not start")
	}
	caddy := NewCaddyAPI(cfg.CaddyAdmin, 5*time.Second)
	newNode := func(build func(XrayInput) ([]byte, error)) *NodeConfig {
		n := NewNodeConfig(cfg, x, caddy, discardLogger())
		if build != nil {
			n.buildXray = build
		}
		n.restartWait = 15 * time.Second
		return n
	}
	serveMaskSite(t)
	serveEcho(t)
	engine := NewEngine(cfg, nil, x, discardLogger())
	engine.setDesired(users(liveUser))
	in := liveNodeInbounds()
	allow := allowLoopback(cfg.XrayAPI)
	node := newNode(nil)

	t.Run("bootstrap wrote the frame and caddy starts empty", func(t *testing.T) {
		disk, err := os.ReadFile(cfg.XrayConfig)
		if err != nil {
			t.Fatal(err)
		}
		frame, _ := BuildXray([]Inbound{}, cfg.XrayAPI)
		if !bytes.Equal(disk, frame) {
			t.Fatalf("bootstrap config = %s", disk)
		}
		current, err := caddy.Config(ctx)
		if err != nil {
			t.Fatal(err)
		}
		empty, _ := BuildCaddy(nil, cfg.CaddyAdmin)
		if !sameJSON(current, empty) {
			t.Fatalf("caddy started with %s", current)
		}
	})

	t.Run("production config keeps users away from node localhost", func(t *testing.T) {
		applied := node.Apply(ctx, in)
		if !reflect.DeepEqual(applied, in) || node.ConfigError() != nil {
			t.Fatalf("applied = %+v, config_error = %v", applied, node.ConfigError())
		}
		if up := liveUptime(t, x); up > 5*time.Second {
			t.Fatalf("xray uptime %v: not restarted", up)
		}
		if err := engine.Reconcile(ctx, applied); err != nil {
			t.Fatal(err)
		}
		if err := echoVia(liveClientA, "must-not-pass"); err == nil {
			t.Fatal("a VPN user reached 127.0.0.1 on the node through the production config")
		}
		eventually(t, "user traffic reached xray before the block", func() error {
			stats, err := x.QueryStats(ctx, "user>>>"+liveUser+"@")
			if err != nil {
				return err
			}
			var uplink int64
			for name, v := range stats {
				if strings.HasSuffix(name, ">>>traffic>>>uplink") {
					uplink += v
				}
			}
			if uplink == 0 {
				return errors.New("no uplink yet: the connection did not get past REALITY")
			}
			return nil
		})
	})

	t.Run("reality with vision goes through caddy by sni", func(t *testing.T) {
		node.buildXray = allow
		applied := node.Apply(ctx, in)
		if !reflect.DeepEqual(applied, in) || node.ConfigError() != nil {
			t.Fatalf("applied = %+v, config_error = %v", applied, node.ConfigError())
		}
		if err := engine.Reconcile(ctx, applied); err != nil {
			t.Fatal(err)
		}
		eventually(t, "alpha via caddy", func() error { return echoVia(liveClientA, "hello-alpha") })
		eventually(t, "beta via caddy", func() error { return echoVia(liveClientB, "hello-beta") })
	})

	t.Run("unknown sni gets the mask site", func(t *testing.T) {
		name, err := peerName("127.0.0.1:443", "scanner.example", false)
		if err != nil || name != "dest.test" {
			t.Fatalf("scanner saw %q (err %v), want the mask site", name, err)
		}
	})

	t.Run("xray inbounds refuse connections without proxy header", func(t *testing.T) {
		if name, err := peerName("127.0.0.1:20001", "alpha.test", true); err != nil || name != "dest.test" {
			t.Fatalf("with PROXY header: %q, %v", name, err)
		}
		if _, err := peerName("127.0.0.1:20001", "alpha.test", false); err == nil {
			t.Fatal("xray inbound accepted a connection that bypassed caddy")
		}
	})

	t.Run("same inbounds leave xray and caddy alone", func(t *testing.T) {
		before := liveUptime(t, x)
		node.Apply(ctx, in)
		restarted := newNode(allow)
		if err := restarted.Load(); err != nil {
			t.Fatal(err)
		}
		if got := restarted.Apply(ctx, in); !reflect.DeepEqual(got, in) {
			t.Fatalf("restarted agent: applied = %+v", got)
		}
		if after := liveUptime(t, x); after < before {
			t.Fatalf("xray restarted: uptime %v -> %v", before, after)
		}
	})

	t.Run("caddy reload keeps an open connection", func(t *testing.T) {
		long, err := net.DialTimeout("tcp", liveClientA, 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = long.Close() }()
		if err := echoOver(long, "before-reload"); err != nil {
			t.Fatal(err)
		}
		extra := append(liveNodeInbounds(), Inbound{
			Tag: "gamma", Port: 443, Protocol: "vless", Transport: "tcp", Security: "reality",
			Reality: &Reality{Dest: "dest.test:28443", ServerNames: []string{"gamma.test"}, ShortIDs: []string{"aa"}, PrivateKey: liveAlphaKey},
		})
		reload, err := BuildCaddy(extra, cfg.CaddyAdmin)
		if err != nil {
			t.Fatal(err)
		}
		if err := caddy.Load(ctx, reload); err != nil {
			t.Fatal(err)
		}
		if err := echoOver(long, "after-reload"); err != nil {
			t.Fatalf("open connection broke on caddy reload: %v", err)
		}
		node.Apply(ctx, in)
		current, err := caddy.Config(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := BuildCaddy(in, cfg.CaddyAdmin)
		if !sameJSON(current, want) {
			t.Fatal("agent did not bring caddy back to the inbounds")
		}
	})

	t.Run("broken config rolls back and the node keeps serving", func(t *testing.T) {
		broken := liveNodeInbounds()
		broken[1].Reality.PrivateKey = "not-a-key"
		applied := node.Apply(ctx, broken)
		if !reflect.DeepEqual(applied, in) {
			t.Fatalf("applied after rollback = %+v", applied)
		}
		e := node.ConfigError()
		if e == nil || !strings.HasPrefix(*e, "xray_config_rejected: ") {
			t.Fatalf("config_error = %v", e)
		}
		disk, _ := os.ReadFile(cfg.XrayConfig)
		want, _ := allow(XrayInput{Inbounds: in})
		if !bytes.Equal(disk, want) {
			t.Fatal("config on disk not rolled back")
		}
		if !WaitForXray(ctx, x, 30*time.Second, 200*time.Millisecond, discardLogger()) {
			t.Fatal("xray did not come back after rollback")
		}
		if err := engine.Reconcile(ctx, applied); err != nil {
			t.Fatal(err)
		}
		eventually(t, "alpha after rollback", func() error { return echoVia(liveClientA, "after-rollback") })
		before := liveUptime(t, x)
		start := time.Now()
		node.Apply(ctx, broken)
		if time.Since(start) > 3*time.Second || liveUptime(t, x) < before {
			t.Fatal("rejected config tried again")
		}
	})

	t.Run("going back to working inbounds clears the error", func(t *testing.T) {
		before := liveUptime(t, x)
		if got := node.Apply(ctx, in); !reflect.DeepEqual(got, in) || node.ConfigError() != nil {
			t.Fatalf("applied = %+v, config_error = %v", got, node.ConfigError())
		}
		if liveUptime(t, x) < before {
			t.Fatal("xray restarted although it already runs these inbounds")
		}
	})

	t.Run("caddy still owns the pid namespace", func(t *testing.T) {
		cmdline, err := os.ReadFile("/proc/1/cmdline")
		if err != nil || !strings.HasPrefix(string(cmdline), "caddy\x00run") {
			t.Fatalf("pid 1 = %q (%v): restarting xray took the namespace owner down", cmdline, err)
		}
	})
}
