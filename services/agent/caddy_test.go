package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type caddyRecorder struct {
	mu       sync.Mutex
	hosts    []string
	bodies   []string
	types    []string
	status   int
	response string
	delay    time.Duration
}

func (c *caddyRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	time.Sleep(c.delay)
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.hosts = append(c.hosts, r.Host)
	c.bodies = append(c.bodies, r.Method+" "+r.URL.Path+" "+string(body))
	c.types = append(c.types, r.Header.Get("Content-Type"))
	status, response := c.status, c.response
	c.mu.Unlock()
	if status != 0 {
		w.WriteHeader(status)
	}
	_, _ = io.WriteString(w, response)
}

func unixCaddy(t *testing.T, h http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "caddy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "admin.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: h}}
	srv.Start()
	t.Cleanup(srv.Close)
	return "unix/" + path + "|0666"
}

func TestCaddyAPIOverUnixSocket(t *testing.T) {
	rec := &caddyRecorder{response: `{"admin":{"listen":"x"}}`}
	api := NewCaddyAPI(unixCaddy(t, rec), time.Second)
	ctx := context.Background()
	got, err := api.Config(ctx)
	if err != nil || string(got) != `{"admin":{"listen":"x"}}` {
		t.Fatalf("config = %s, err = %v", got, err)
	}
	if err := api.Load(ctx, []byte(`{"apps":{}}`)); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.bodies[0] != "GET /config/ " || rec.bodies[1] != `POST /load {"apps":{}}` || rec.types[1] != "application/json" {
		t.Fatalf("requests = %q, types = %q", rec.bodies, rec.types)
	}
}

func TestCaddyAPIOverTCPSendsItsAddressAsHost(t *testing.T) {
	rec := &caddyRecorder{response: "null"}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")
	if _, err := NewCaddyAPI(addr, time.Second).Config(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.hosts[0] != addr {
		t.Fatalf("host = %q, want %q: caddy checks Host on TCP admin", rec.hosts[0], addr)
	}
}

func TestCaddyLoadRejectionCarriesCaddyError(t *testing.T) {
	rec := &caddyRecorder{status: http.StatusBadRequest, response: `{"error":"loading config: unknown module: layer4.handlers.nope"}`}
	err := NewCaddyAPI(unixCaddy(t, rec), time.Second).Load(context.Background(), []byte(`{}`))
	var rejected *CaddyRejectedError
	if !errors.As(err, &rejected) || rejected.Status != 400 || rejected.Message != "loading config: unknown module: layer4.handlers.nope" {
		t.Fatalf("err = %v", err)
	}
}

func TestCaddyUnreachableIsNotARejection(t *testing.T) {
	err := NewCaddyAPI("unix//nonexistent/admin.sock", time.Second).Load(context.Background(), []byte(`{}`))
	var rejected *CaddyRejectedError
	if err == nil || errors.As(err, &rejected) {
		t.Fatalf("err = %v, want a transport error", err)
	}
	if _, err := NewCaddyAPI("unix//nonexistent/admin.sock", time.Second).Config(context.Background()); err == nil {
		t.Fatal("config from a missing socket")
	}
}

func TestCaddyAPITimesOut(t *testing.T) {
	rec := &caddyRecorder{delay: 500 * time.Millisecond}
	start := time.Now()
	if _, err := NewCaddyAPI(unixCaddy(t, rec), 50*time.Millisecond).Config(context.Background()); err == nil {
		t.Fatal("want timeout")
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Fatal("timeout not applied")
	}
}

func TestSameJSONIgnoresFormatting(t *testing.T) {
	if !sameJSON([]byte(`{"a":1,"b":[1,2]}`), []byte("{\n  \"b\": [1, 2],\n  \"a\": 1\n}\n")) {
		t.Fatal("same document in other formatting")
	}
	if sameJSON([]byte(`{"b":[2,1]}`), []byte(`{"b":[1,2]}`)) || sameJSON([]byte(`null`), []byte(`{}`)) || sameJSON([]byte(`{`), []byte(`{`)) {
		t.Fatal("different documents compared equal")
	}
}
