package panel

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type logLine map[string]any

func logLines(t *testing.T, buf *bytes.Buffer) []logLine {
	t.Helper()
	var out []logLine
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var l logLine
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("log line %q: %v", raw, err)
		}
		out = append(out, l)
	}
	return out
}

func findLog(lines []logLine, msg, level string) logLine {
	for _, l := range lines {
		if l["msg"] == msg && l["level"] == level {
			return l
		}
	}
	return nil
}

func accessLine(t *testing.T, buf *bytes.Buffer, requestID string) logLine {
	t.Helper()
	for _, l := range logLines(t, buf) {
		if l["msg"] == "http_request" && l["request_id"] == requestID {
			return l
		}
	}
	t.Fatalf("no access line for %s in %s", requestID, buf)
	return nil
}

func TestProxyFailuresAreErrorsInAccessLog(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	e.core.status = http.StatusUnauthorized
	w := e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(sid))
	l := accessLine(t, e.logs, w.Header().Get("X-Request-Id"))
	if l["level"] != "ERROR" || !strings.Contains(l["error"].(string), "rejected the panel certificate") {
		t.Fatalf("core 401 access line = %v", l)
	}
	if p := findLog(logLines(t, e.logs), "panel_proxy", "WARN"); p == nil || p["status"] != float64(http.StatusBadGateway) {
		t.Fatalf("panel_proxy for a failed call must be WARN: %s", e.logs)
	}

	e.srv.Close()
	w = e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(sid))
	l = accessLine(t, e.logs, w.Header().Get("X-Request-Id"))
	if l["level"] != "ERROR" || !strings.Contains(l["error"].(string), "core unavailable") {
		t.Fatalf("core down access line = %v", l)
	}
}

func TestProxyCore5xxIsWarn(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	e.core.status = http.StatusInternalServerError
	e.core.reply = `{"code":"internal","message":"boom"}`
	w := e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(sid))
	lines := logLines(t, e.logs)
	p := findLog(lines, "panel_proxy", "WARN")
	if p == nil || p["request_id"] != w.Header().Get("X-Request-Id") {
		t.Fatalf("core 5xx must leave a WARN panel_proxy with the request id: %s", e.logs)
	}
	e.core.status = http.StatusNotFound
	e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(sid))
	var infos int
	for _, l := range logLines(t, e.logs) {
		if l["msg"] == "panel_proxy" && l["level"] == "INFO" && l["status"] == float64(http.StatusNotFound) {
			infos++
		}
	}
	if infos != 1 {
		t.Fatalf("core 4xx must stay INFO: %s", e.logs)
	}
}

func TestCSRFRefusalIsLogged(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	hdr := map[string]string{"Cookie": sessionCookie + "=" + sid, "X-Sylph-Panel": "1", "Origin": "https://evil.example"}
	w := e.do("POST", "/api/core/v1/admin/users/1/ban?q=SECRETSUBTOKEN", `{}`, hdr)
	if w.Code != http.StatusForbidden {
		t.Fatalf("csrf %d", w.Code)
	}
	l := findLog(logLines(t, e.logs), "panel_csrf_refused", "WARN")
	if l == nil {
		t.Fatalf("csrf refusal not logged: %s", e.logs)
	}
	if l["reason"] != "origin" || l["origin"] != "https://evil.example" || l["host"] != "example.com" || l["ip"] != "203.0.113.7" {
		t.Fatalf("csrf line = %v", l)
	}
	if l["request_id"] != w.Header().Get("X-Request-Id") {
		t.Fatalf("csrf line request id = %v", l["request_id"])
	}
	if strings.Contains(e.logs.String(), "SECRETSUBTOKEN") {
		t.Fatalf("query string leaked into logs: %s", e.logs)
	}
	for reason, h := range map[string]map[string]string{
		"header":         {"Cookie": sessionCookie + "=" + sid},
		"sec_fetch_site": {"Cookie": sessionCookie + "=" + sid, "X-Sylph-Panel": "1", "Sec-Fetch-Site": "cross-site"},
	} {
		e.logs.Reset()
		e.do("POST", "/api/core/v1/admin/users/1/ban", `{}`, h)
		if l := findLog(logLines(t, e.logs), "panel_csrf_refused", "WARN"); l == nil || l["reason"] != reason {
			t.Errorf("%s: %v", reason, l)
		}
	}
}

type failingWriter struct {
	h      http.Header
	status int
}

func (f *failingWriter) Header() http.Header {
	if f.h == nil {
		f.h = http.Header{}
	}
	return f.h
}

func (f *failingWriter) WriteHeader(code int) { f.status = code }

func (f *failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("client went away")
}

func serveFailing(h http.Handler, r *http.Request) {
	r.RemoteAddr = "203.0.113.7:4444"
	h.ServeHTTP(&failingWriter{}, r)
}

func TestWriteFailuresAreRecorded(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	cases := map[string]*http.Request{
		"proxy reply": func() *http.Request {
			r := httptest.NewRequest("GET", "/api/core/v1/admin/overview", nil)
			r.Header.Set("Cookie", sessionCookie+"="+sid)
			return r
		}(),
		"spa index": httptest.NewRequest("GET", "/users/1", nil),
	}
	for name, r := range cases {
		e.logs.Reset()
		serveFailing(e.h, r)
		l := findLog(logLines(t, e.logs), "http_request", "ERROR")
		if l == nil || !strings.Contains(l["error"].(string), "client went away") {
			t.Errorf("%s: write failure not recorded: %s", name, e.logs)
		}
	}

	logs := &bytes.Buffer{}
	auth, _ := newTestAuth(t)
	proxy, _ := NewCoreProxy("http://127.0.0.1:1", http.DefaultClient, slog.New(slog.DiscardHandler))
	trust, _ := parseTrustedProxies("")
	h := NewServer(auth, proxy, trust, nil, time.Hour, slog.New(slog.NewJSONHandler(logs, nil))).Handler()
	serveFailing(h, httptest.NewRequest("GET", "/", nil))
	if l := findLog(logLines(t, logs), "http_request", "ERROR"); l == nil || !strings.Contains(l["error"].(string), "client went away") {
		t.Fatalf("unbuilt page write failure not recorded: %s", logs)
	}
}

func TestMissingIndexIsAnError(t *testing.T) {
	logs := &bytes.Buffer{}
	auth, _ := newTestAuth(t)
	proxy, _ := NewCoreProxy("http://127.0.0.1:1", http.DefaultClient, slog.New(slog.DiscardHandler))
	trust, _ := parseTrustedProxies("")
	ui := fstest.MapFS{"assets/app.js": {Data: []byte("1")}}
	h := NewServer(auth, proxy, trust, ui, time.Hour, slog.New(slog.NewJSONHandler(logs, nil))).Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/users/1", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("missing index %d", w.Code)
	}
	if l := findLog(logLines(t, logs), "http_request", "ERROR"); l == nil || !strings.Contains(l["error"].(string), "index.html") {
		t.Fatalf("missing index not recorded: %s", logs)
	}
}
