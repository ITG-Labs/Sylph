package panel

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

type coreHit struct {
	method, path, query string
	header              http.Header
	body                []byte
}

type fakeCore struct {
	mu     sync.Mutex
	hits   []coreHit
	status int
	reply  string
}

func (f *fakeCore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.hits = append(f.hits, coreHit{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Clone(), b})
	status, reply := f.status, f.reply
	f.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	if reply == "" {
		reply = `{"ok":true}`
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Set-Cookie", "evil=1")
	w.WriteHeader(status)
	io.WriteString(w, reply)
}

func (f *fakeCore) calls() []coreHit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]coreHit(nil), f.hits...)
}

type testEnv struct {
	t     *testing.T
	h     http.Handler
	core  *fakeCore
	srv   *httptest.Server
	auth  *Auth
	clock *fakeClock
	logs  *bytes.Buffer
}

var testUI = fstest.MapFS{
	"index.html":           {Data: []byte("<!doctype html><title>Sylph</title>")},
	"assets/app-abc123.js": {Data: []byte("console.log(1)")},
	"favicon.svg":          {Data: []byte("<svg/>")},
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	core := &fakeCore{}
	srv := httptest.NewServer(core)
	t.Cleanup(srv.Close)
	auth, clock := newTestAuth(t)
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	proxy, err := NewCoreProxy(srv.URL, srv.Client(), logger)
	if err != nil {
		t.Fatal(err)
	}
	trust, _ := parseTrustedProxies("")
	s := NewServer(auth, proxy, trust, testUI, 12*time.Hour, logger)
	return &testEnv{t: t, h: s.Handler(), core: core, srv: srv, auth: auth, clock: clock, logs: logs}
}

func (e *testEnv) do(method, path string, body string, hdr map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	r.RemoteAddr = "203.0.113.7:4444"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *testEnv) login(name, password string) string {
	e.t.Helper()
	code := totpCode(rfcSecret, e.clock.now().Unix()/totpPeriod, totpDigits)
	body, _ := json.Marshal(map[string]string{"name": name, "password": password, "code": code})
	w := e.do("POST", "/auth/login", string(body), map[string]string{"X-Sylph-Panel": "1"})
	if w.Code != http.StatusOK {
		e.t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	e.t.Fatal("no session cookie")
	return ""
}

func panelHdr(session string, extra ...string) map[string]string {
	h := map[string]string{"Cookie": sessionCookie + "=" + session, "X-Sylph-Panel": "1"}
	for i := 0; i+1 < len(extra); i += 2 {
		h[extra[i]] = extra[i+1]
	}
	return h
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body %q: %v", w.Body, err)
	}
	return e.Code
}

func TestHealthz(t *testing.T) {
	e := newEnv(t)
	if w := e.do("GET", "/healthz", "", nil); w.Code != http.StatusOK {
		t.Fatalf("healthz %d", w.Code)
	}
}

func TestLoginSetsStrictCookie(t *testing.T) {
	e := newEnv(t)
	code := totpCode(rfcSecret, e.clock.now().Unix()/totpPeriod, totpDigits)
	w := e.do("POST", "/auth/login", `{"name":"Иван","password":"secret-1","code":"`+code+`"}`, map[string]string{"X-Sylph-Panel": "1"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"Иван"`) {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	var c *http.Cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == sessionCookie {
			c = ck
		}
	}
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 12*3600 || c.Domain != "" {
		t.Fatalf("cookie %+v", c)
	}
	if w := e.do("GET", "/auth/me", "", map[string]string{"Cookie": sessionCookie + "=" + c.Value}); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"Иван"`) {
		t.Fatalf("me %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/auth/me", "", nil); w.Code != http.StatusUnauthorized || errCode(t, w) != "unauthorized" {
		t.Fatalf("me without session %d", w.Code)
	}
	if !strings.Contains(e.logs.String(), `"msg":"panel_login"`) || strings.Contains(e.logs.String(), "secret-1") || strings.Contains(e.logs.String(), code) {
		t.Fatalf("login log: %s", e.logs)
	}
}

func TestLoginErrors(t *testing.T) {
	e := newEnv(t)
	h := map[string]string{"X-Sylph-Panel": "1"}
	if w := e.do("POST", "/auth/login", `{"name":"Иван","password":"nope","code":"000000"}`, h); w.Code != http.StatusUnauthorized || errCode(t, w) != "bad_credentials" {
		t.Fatalf("bad creds %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{`, `{"name":"Иван","password":"x","code":"1","extra":1}`, `[]`, `{"name":1}`, `{} {}`} {
		if w := e.do("POST", "/auth/login", body, h); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
	big := `{"name":"` + strings.Repeat("a", 8<<10) + `","password":"x","code":"1"}`
	if w := e.do("POST", "/auth/login", big, h); w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big body %d", w.Code)
	}
	if w := e.do("GET", "/auth/login", "", nil); w.Code == http.StatusOK {
		t.Fatal("GET login succeeded")
	}
}

func TestLoginLimiterUsesForwardedIP(t *testing.T) {
	e := newEnv(t)
	try := func(xff, password string) int {
		code := totpCode(rfcSecret, e.clock.now().Unix()/totpPeriod, totpDigits)
		body, _ := json.Marshal(map[string]string{"name": "Иван", "password": password, "code": code})
		r := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(body))
		r.RemoteAddr = "127.0.0.1:5000"
		r.Header.Set("X-Forwarded-For", xff)
		r.Header.Set("X-Sylph-Panel", "1")
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		return w.Code
	}
	for range loginMaxFailures {
		if c := try("198.51.100.1", "nope"); c != http.StatusUnauthorized {
			t.Fatalf("fail attempt %d", c)
		}
	}
	if c := try("198.51.100.1", "secret-1"); c != http.StatusTooManyRequests {
		t.Fatalf("blocked client got %d", c)
	}
	if c := try("198.51.100.2", "secret-1"); c != http.StatusOK {
		t.Fatalf("other client got %d", c)
	}
}

func TestLogout(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	if w := e.do("POST", "/auth/logout", "", map[string]string{"Cookie": sessionCookie + "=" + sid}); w.Code != http.StatusForbidden {
		t.Fatalf("logout without csrf header %d", w.Code)
	}
	w := e.do("POST", "/auth/logout", "", panelHdr(sid))
	if w.Code != http.StatusOK {
		t.Fatalf("logout %d", w.Code)
	}
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("cookie not cleared")
	}
	if w := e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(sid)); w.Code != http.StatusUnauthorized {
		t.Fatalf("session alive after logout: %d", w.Code)
	}
}

func TestProxyRequiresSession(t *testing.T) {
	e := newEnv(t)
	if w := e.do("GET", "/api/core/v1/admin/overview", "", nil); w.Code != http.StatusUnauthorized || errCode(t, w) != "unauthorized" {
		t.Fatalf("no session %d", w.Code)
	}
	if w := e.do("GET", "/api/core/v1/admin/overview", "", panelHdr("forged")); w.Code != http.StatusUnauthorized {
		t.Fatalf("forged session %d", w.Code)
	}
	sid := e.login("Иван", "secret-1")
	e.clock.add(12*time.Hour + time.Second)
	if w := e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(sid)); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session %d", w.Code)
	}
	if n := len(e.core.calls()); n != 0 {
		t.Fatalf("core called %d times", n)
	}
}

func TestProxyForwardsWithOperatorFromSession(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	e.core.status = http.StatusConflict
	e.core.reply = `{"code":"conflict","field":"user_id","message":"banned"}`
	w := e.do("POST", "/api/core/v1/admin/users/42/extend?x=1", `{"days":7}`, panelHdr(sid,
		"X-Sylph-Operator", "Злоумышленник",
		"Authorization", "Bearer x",
		"X-Forwarded-For", "6.6.6.6",
		"X-Request-Id", "req-123",
		"Content-Type", "text/plain",
	))
	if w.Code != http.StatusConflict || w.Body.String() != e.core.reply {
		t.Fatalf("passthrough %d %s", w.Code, w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("response headers %v", w.Header())
	}
	calls := e.core.calls()
	if len(calls) != 1 {
		t.Fatalf("%d core calls", len(calls))
	}
	c := calls[0]
	if c.method != "POST" || c.path != "/v1/admin/users/42/extend" || c.query != "x=1" || string(c.body) != `{"days":7}` {
		t.Fatalf("core got %+v", c)
	}
	if ops := c.header.Values("X-Sylph-Operator"); len(ops) != 1 || ops[0] != "Иван" {
		t.Fatalf("operator %v", ops)
	}
	if c.header.Get("Content-Type") != "application/json" || c.header.Get("X-Request-Id") != "req-123" {
		t.Fatalf("headers %v", c.header)
	}
	for _, h := range []string{"Cookie", "Authorization", "X-Forwarded-For", "X-Sylph-Panel"} {
		if c.header.Get(h) != "" {
			t.Errorf("%s leaked to core", h)
		}
	}
	logs := e.logs.String()
	if !strings.Contains(logs, `"msg":"panel_proxy"`) || !strings.Contains(logs, `"route":"POST /v1/admin/users/{id}/extend"`) || strings.Contains(logs, "x=1") {
		t.Fatalf("proxy log: %s", logs)
	}
}

func TestProxyEachAdminSeesOwnName(t *testing.T) {
	e := newEnv(t)
	a := e.login("Иван", "secret-1")
	b := e.login("Олег", "secret-2")
	e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(a))
	e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(b))
	calls := e.core.calls()
	if len(calls) != 2 || calls[0].header.Get("X-Sylph-Operator") != "Иван" || calls[1].header.Get("X-Sylph-Operator") != "Олег" {
		t.Fatalf("operators %v %v", calls[0].header.Get("X-Sylph-Operator"), calls[1].header.Get("X-Sylph-Operator"))
	}
}

func TestProxyRejectsUnlistedPaths(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/core/v1/sync/users"},
		{"POST", "/api/core/v1/subscription"},
		{"POST", "/api/core/v1/users/by-token"},
		{"POST", "/api/core/v1/payments/create"},
		{"POST", "/api/core/v1/maintenance/expire"},
		{"GET", "/api/core/v1/admin/users/abc"},
		{"GET", "/api/core/v1/admin/payments/a%2F..%2Fb"},
		{"DELETE", "/api/core/v1/admin/users/1"},
		{"GET", "/api/core/health"},
		{"GET", "/api/core/"},
		{"GET", "/api/other"},
	} {
		w := e.do(tc.method, tc.path, "", panelHdr(sid))
		if w.Code != http.StatusNotFound || errCode(t, w) != "not_found" {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	if n := len(e.core.calls()); n != 0 {
		t.Fatalf("core called %d times", n)
	}
}

func TestProxyBodyLimit(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	exact := `{"reason":"` + strings.Repeat("a", maxBodyBytes-13) + `"}`
	if len(exact) != maxBodyBytes {
		t.Fatalf("len %d", len(exact))
	}
	if w := e.do("POST", "/api/core/v1/admin/users/1/ban", exact, panelHdr(sid)); w.Code != http.StatusOK {
		t.Fatalf("64 KiB body: %d", w.Code)
	}
	if w := e.do("POST", "/api/core/v1/admin/users/1/ban", exact+" ", panelHdr(sid)); w.Code != http.StatusRequestEntityTooLarge || errCode(t, w) != "too_large" {
		t.Fatalf("64 KiB + 1: %d", w.Code)
	}
	if n := len(e.core.calls()); n != 1 {
		t.Fatalf("core called %d times", n)
	}
}

func TestProxyCoreFailures(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	e.core.status = http.StatusUnauthorized
	e.core.reply = "revoked"
	if w := e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(sid)); w.Code != http.StatusBadGateway || errCode(t, w) != "core_rejected" {
		t.Fatalf("core 401 → %d %s", w.Code, w.Body)
	}
	e.srv.Close()
	if w := e.do("GET", "/api/core/v1/admin/overview", "", panelHdr(sid)); w.Code != http.StatusBadGateway || errCode(t, w) != "core_unavailable" {
		t.Fatalf("core down → %d %s", w.Code, w.Body)
	}
}

func TestCSRFGuardsMutations(t *testing.T) {
	e := newEnv(t)
	sid := e.login("Иван", "secret-1")
	cookie := sessionCookie + "=" + sid
	for name, hdr := range map[string]map[string]string{
		"no header":         {"Cookie": cookie},
		"wrong header":      {"Cookie": cookie, "X-Sylph-Panel": "yes"},
		"foreign origin":    {"Cookie": cookie, "X-Sylph-Panel": "1", "Origin": "https://evil.example"},
		"null origin":       {"Cookie": cookie, "X-Sylph-Panel": "1", "Origin": "null"},
		"cross-site fetch":  {"Cookie": cookie, "X-Sylph-Panel": "1", "Sec-Fetch-Site": "cross-site"},
		"same-site sibling": {"Cookie": cookie, "X-Sylph-Panel": "1", "Sec-Fetch-Site": "same-site"},
	} {
		w := e.do("POST", "/api/core/v1/admin/users/1/ban", `{}`, hdr)
		if w.Code != http.StatusForbidden || errCode(t, w) != "csrf" {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if n := len(e.core.calls()); n != 0 {
		t.Fatalf("core called %d times", n)
	}
	ok := panelHdr(sid, "Origin", "http://example.com", "Sec-Fetch-Site", "same-origin")
	if w := e.do("POST", "/api/core/v1/admin/users/1/ban", `{}`, ok); w.Code != http.StatusOK {
		t.Fatalf("same origin: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/core/v1/admin/overview", "", map[string]string{"Cookie": cookie}); w.Code != http.StatusOK {
		t.Fatalf("GET without header: %d", w.Code)
	}
	if w := e.do("POST", "/auth/login", `{"name":"Иван","password":"secret-1","code":"000000"}`, map[string]string{"Origin": "https://evil.example", "X-Sylph-Panel": "1"}); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login: %d", w.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/users/1", "/api/core/v1/admin/overview", "/healthz"} {
		w := e.do("GET", p, "", nil)
		h := w.Header()
		csp := h.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "unsafe-eval") {
			t.Errorf("%s csp %q", p, csp)
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s headers %v", p, h)
		}
	}
}

func TestServesSPA(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/users/42", "/payments?status=pending", "/nodes/1/inbounds"} {
		w := e.do("GET", p, "", nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<title>Sylph</title>") || w.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d %q %q", p, w.Code, w.Body, w.Header().Get("Cache-Control"))
		}
		if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s content-type %q", p, w.Header().Get("Content-Type"))
		}
	}
	w := e.do("GET", "/assets/app-abc123.js", "", nil)
	if w.Code != http.StatusOK || w.Body.String() != "console.log(1)" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	if w := e.do("GET", "/favicon.svg", "", nil); w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("favicon %d", w.Code)
	}
	for _, p := range []string{"/assets/missing.js", "/robots.txt"} {
		if w := e.do("GET", p, "", nil); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if w := e.do("POST", "/", "", map[string]string{"X-Sylph-Panel": "1"}); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / %d", w.Code)
	}
}

func TestUnbuiltUI(t *testing.T) {
	auth, _ := newTestAuth(t)
	proxy, _ := NewCoreProxy("http://127.0.0.1:1", http.DefaultClient, slog.New(slog.DiscardHandler))
	trust, _ := parseTrustedProxies("")
	h := NewServer(auth, proxy, trust, nil, time.Hour, slog.New(slog.DiscardHandler)).Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unbuilt ui %d", w.Code)
	}
}
