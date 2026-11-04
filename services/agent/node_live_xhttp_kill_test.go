package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/agent/internal/xraygrpc"
)

const (
	liveNeighbour       = "2c1f3f0a-8b0e-4a53-9d11-5f0a3b7c9e21"
	liveNeighbourClient = "client2:10801"
)

type liveSession struct {
	name string
	conn net.Conn
	dead atomic.Bool
	sent atomic.Int64
}

func (s *liveSession) pump() {
	go func() {
		for {
			if err := echoOver(s.conn, "ping-ping-ping"); err != nil {
				s.dead.Store(true)
				return
			}
			s.sent.Add(1)
			time.Sleep(100 * time.Millisecond)
		}
	}()
}

func openLiveSession(t *testing.T, name, addr string) *liveSession {
	t.Helper()
	s := &liveSession{name: name}
	within(t, name, 30*time.Second, func() error {
		c, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			return err
		}
		if err := echoOver(c, "hello-"+name); err != nil {
			_ = c.Close()
			return err
		}
		s.conn = c
		return nil
	})
	t.Cleanup(func() { _ = s.conn.Close() })
	s.pump()
	return s
}

func serveCountedEcho(t *testing.T) *atomic.Int64 {
	t.Helper()
	ln, err := net.Listen("tcp", liveEcho)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	open := &atomic.Int64{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			open.Add(1)
			go func() {
				defer open.Add(-1)
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return open
}

func TestNodeLiveRemovedUserLosesOpenSessions(t *testing.T) {
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
	serveDecoy(t)
	echoes := serveCountedEcho(t)
	node := NewNodeConfig(cfg, x, NewCaddyAPI(cfg.CaddyAdmin, 5*time.Second), discardLogger())
	node.buildXray = allowLoopback(cfg.XrayAPI)
	node.restartWait = 15 * time.Second
	var logs bytes.Buffer
	engine := NewEngine(cfg, nil, x, slog.New(slog.NewTextHandler(&logs, nil)))
	engine.setDesired(users(liveUser, liveNeighbour))
	in := liveXHTTPInbounds()
	applied := node.Apply(ctx, in)
	if !reflect.DeepEqual(applied, in) || node.ConfigError() != nil {
		t.Fatalf("applied = %+v, config_error = %v", applied, node.ConfigError())
	}
	engine.inbounds = applied
	if err := engine.Reconcile(ctx, applied); err != nil {
		t.Fatal(err)
	}
	within(t, "certificate from the ACME server", 60*time.Second, func() error {
		if st := probeCerts(ctx, tlsDomains(applied))[liveCDNDomain]; st.NotAfter == nil {
			return fmt.Errorf("status %+v", st)
		}
		return nil
	})

	sessions := []*liveSession{
		openLiveSession(t, "raw-vision", liveClientA),
		openLiveSession(t, "xhttp-packet-up", liveCDNPacket),
		openLiveSession(t, "xhttp-stream-one", liveCDNOne),
		openLiveSession(t, "xhttp-stream-up", liveCDNUp),
		openLiveSession(t, "xhttp-reality", liveRealityXH),
	}
	neighbour := openLiveSession(t, "neighbour", liveNeighbourClient)
	time.Sleep(time.Second)
	uptime := liveUptime(t, x)

	engine.setDesired(users(liveNeighbour))
	if err := engine.Reconcile(ctx, applied); err != nil {
		t.Fatal(err)
	}

	t.Run("every mode of a removed user ends at once", func(t *testing.T) {
		within(t, "sessions of the removed user", 5*time.Second, func() error {
			for _, s := range sessions {
				if !s.dead.Load() {
					return fmt.Errorf("%s still passes traffic, logs: %s", s.name, logs.String())
				}
			}
			return nil
		})
		if !strings.Contains(logs.String(), "sessions_killed") {
			t.Fatalf("logs = %s", logs.String())
		}
		t.Log(strings.TrimSpace(logs.String()))
	})

	t.Run("xray lets go of the upstreams and forgets the user", func(t *testing.T) {
		within(t, "only the neighbour's upstream left", 10*time.Second, func() error {
			if n := echoes.Load(); n != 1 {
				return fmt.Errorf("open upstreams = %d", n)
			}
			online, err := x.OnlineIPs(ctx)
			if err != nil {
				return err
			}
			for email := range online {
				if strings.HasPrefix(email, liveUser) {
					return fmt.Errorf("still online: %s", email)
				}
			}
			return nil
		})
	})

	t.Run("counters keep what passed and stop growing", func(t *testing.T) {
		first, err := x.QueryStats(ctx, "user>>>"+liveUser+"@")
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range []string{"alpha", "cdn", "rx"} {
			if first["user>>>"+userEmail(liveUser, tag)+">>>traffic>>>uplink"] == 0 {
				t.Fatalf("no uplink on %s: %v", tag, first)
			}
		}
		time.Sleep(2 * time.Second)
		second, err := x.QueryStats(ctx, "user>>>"+liveUser+"@")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("counters of a removed user moved: %v -> %v", first, second)
		}
	})

	t.Run("the neighbour on another address keeps its session", func(t *testing.T) {
		before := neighbour.sent.Load()
		within(t, "neighbour traffic", 5*time.Second, func() error {
			if neighbour.dead.Load() {
				return fmt.Errorf("neighbour session died")
			}
			if neighbour.sent.Load() <= before {
				return fmt.Errorf("neighbour stopped")
			}
			return nil
		})
	})

	t.Run("the removed user cannot come back", func(t *testing.T) {
		if err := echoVia(liveClientA, "back"); err == nil {
			t.Fatal("removed user reconnected")
		}
	})

	t.Run("xray was not restarted and the agent still reaches it", func(t *testing.T) {
		if after := liveUptime(t, x); after < uptime {
			t.Fatalf("xray restarted: uptime %v -> %v", uptime, after)
		}
		if _, err := NewCaddyAPI(cfg.CaddyAdmin, 5*time.Second).Config(ctx); err != nil {
			t.Fatalf("caddy admin: %v", err)
		}
	})
}
