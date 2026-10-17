package healthcheck

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()
	sick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer sick.Close()
	var out bytes.Buffer
	if code := Run(ok.URL, &out); code != 0 {
		t.Fatalf("healthy = %d (%s)", code, out.String())
	}
	out.Reset()
	if code := Run(sick.URL, &out); code != 1 || !strings.Contains(out.String(), "503") {
		t.Fatalf("sick = %d (%s)", code, out.String())
	}
	out.Reset()
	if code := Run("http://127.0.0.1:1/health", &out); code != 1 || out.Len() == 0 {
		t.Fatalf("down = %d (%s)", code, out.String())
	}
}

func TestLocalURL(t *testing.T) {
	cases := map[string]string{
		":8080":          "http://127.0.0.1:8080/health",
		"127.0.0.1:8081": "http://127.0.0.1:8081/health",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/health",
		"[::]:9000":      "http://127.0.0.1:9000/health",
	}
	for listen, want := range cases {
		if got := LocalURL(listen, "/health"); got != want {
			t.Errorf("LocalURL(%q) = %q, want %q", listen, got, want)
		}
	}
}
