package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type release struct {
	tag    string
	binary []byte
	sums   string
}

func serve(t *testing.T, r release) (*Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /latest", func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasPrefix(req.Header.Get("User-Agent"), "sylph/") {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		fmt.Fprintf(w, `{"tag_name":%q,"draft":false,"prerelease":false}`, r.tag)
	})
	mux.HandleFunc("GET /download/{tag}/{name}", func(w http.ResponseWriter, req *http.Request) {
		if req.PathValue("tag") != r.tag {
			http.NotFound(w, req)
			return
		}
		switch req.PathValue("name") {
		case Asset:
			w.Write(r.binary)
		case Sums:
			w.Write([]byte(r.sums))
		default:
			http.NotFound(w, req)
		}
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	c := New("0.2.0")
	c.HTTP = srv.Client()
	c.API = srv.URL + "/latest"
	c.Download = srv.URL + "/download"
	return c, srv
}

func sumLine(data []byte, name string) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:]) + "  " + name + "\n"
}

func TestLatest(t *testing.T) {
	c, _ := serve(t, release{tag: "v0.3.0"})
	got, err := c.Latest(context.Background())
	if err != nil || got != "0.3.0" {
		t.Fatalf("Latest = %q, %v", got, err)
	}
	for _, tag := range []string{"edge-abc1234", "0.3.0", "v0.3", "v0.3.0-rc1", "v0.3.0\n", ""} {
		c, _ := serve(t, release{tag: tag})
		if v, err := c.Latest(context.Background()); err == nil {
			t.Fatalf("tag %q accepted as %q", tag, v)
		}
	}
}

func TestLatestHTTPError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()
	c := New("0.2.0")
	c.HTTP, c.API = srv.Client(), srv.URL
	if _, err := c.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchVerifiesChecksum(t *testing.T) {
	bin := []byte("#!/bin/sh\necho new\n")
	c, _ := serve(t, release{tag: "v0.3.0", binary: bin, sums: "deadbeef  other\n" + sumLine(bin, Asset)})
	dir := t.TempDir()
	path, err := c.Fetch(context.Background(), "0.3.0", dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("fetched into %s, want %s", path, dir)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(bin) {
		t.Fatalf("content %q, %v", data, err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v, %v", st.Mode(), err)
	}
}

func TestFetchRejectsBadDownloads(t *testing.T) {
	bin := []byte("binary")
	cases := map[string]release{
		"checksum mismatch": {tag: "v0.3.0", binary: bin, sums: sumLine([]byte("other"), Asset)},
		"asset not in sums": {tag: "v0.3.0", binary: bin, sums: sumLine(bin, "sylph-linux-arm64")},
		"garbage sums":      {tag: "v0.3.0", binary: bin, sums: "not a checksum file"},
		"missing release":   {tag: "v0.4.0", binary: bin, sums: sumLine(bin, Asset)},
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := serve(t, r)
			dir := t.TempDir()
			if _, err := c.Fetch(context.Background(), "0.3.0", dir); err == nil {
				t.Fatal("bad download accepted")
			}
			if left, _ := os.ReadDir(dir); len(left) != 0 {
				t.Fatalf("left behind: %v", left)
			}
		})
	}
	c, _ := serve(t, release{tag: "v0.3.0", binary: bin, sums: sumLine([]byte("other"), Asset)})
	if _, err := c.Fetch(context.Background(), "0.3.0", t.TempDir()); !errors.Is(err, ErrChecksum) {
		t.Fatalf("mismatch must be ErrChecksum: %v", err)
	}
}

func TestFetchCapsSize(t *testing.T) {
	big := make([]byte, 2048)
	c, _ := serve(t, release{tag: "v0.3.0", binary: big, sums: sumLine(big, Asset)})
	c.MaxSize = 1024
	dir := t.TempDir()
	if _, err := c.Fetch(context.Background(), "0.3.0", dir); err == nil || errors.Is(err, ErrChecksum) || !strings.Contains(err.Error(), "больше") {
		t.Fatalf("oversized binary: %v", err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

func TestRedirectsStayOnHTTPS(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	}))
	defer plain.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer srv.Close()
	c := New("0.2.0")
	c.HTTP = srv.Client()
	c.HTTP.CheckRedirect = New("0.2.0").HTTP.CheckRedirect
	c.API = srv.URL
	if v, err := c.Latest(context.Background()); err == nil {
		t.Fatalf("followed a redirect to http: %q", v)
	}
}

func fakeBinary(t *testing.T, dir string, prints string) string {
	t.Helper()
	path := filepath.Join(dir, ".sylph-new-test")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n[ \"$1\" = version ] && echo '"+prints+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstallSwapsOnlyAWorkingBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sylph")
	if err := os.WriteFile(exe, []byte("old"), 0o750); err != nil {
		t.Fatal(err)
	}
	bad := fakeBinary(t, dir, "sylph 0.2.9 (образы: 0.2.9)")
	if err := Install(context.Background(), bad, exe, "0.3.0"); err == nil {
		t.Fatal("binary reporting a different version installed")
	}
	if data, _ := os.ReadFile(exe); string(data) != "old" {
		t.Fatalf("old binary replaced: %q", data)
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatalf("rejected binary left behind: %v", err)
	}
	good := fakeBinary(t, dir, "sylph 0.3.0 (образы: 0.3.0)")
	if err := Install(context.Background(), good, exe, "0.3.0"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(exe)
	if !strings.Contains(string(data), "sylph 0.3.0") {
		t.Fatalf("not swapped: %q", data)
	}
	if st, err := os.Stat(exe); err != nil || st.Mode().Perm() != 0o750 {
		t.Fatalf("mode of the old binary not kept: %v, %v", st.Mode(), err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".sylph-new-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}
