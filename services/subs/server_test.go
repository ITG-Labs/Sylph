package subs

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

const testToken = "82781f94d5ba0d5d8dd48fae364fa8da"

type fakeCore struct {
	mu    sync.Mutex
	view  *View
	err   error
	calls []Request
	ids   []string
}

func (f *fakeCore) Subscription(ctx context.Context, req Request) (*View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	f.ids = append(f.ids, logging.RequestIDFromContext(ctx))
	if f.err != nil {
		return nil, f.err
	}
	v := *f.view
	return &v, nil
}

func okView() *View {
	limit := int64(107374182400)
	expires := time.Date(2026, 11, 11, 6, 21, 27, 0, time.UTC)
	return &View{Status: StatusOK, TrafficUsedBytes: 5368709120, TrafficLimitBytes: &limit, ExpiresAt: &expires, Connections: goldenConnections(),
		Title: "Sylph VPN", SupportURL: "https://t.me/sylph_support"}
}

func testSettings() Settings {
	return Settings{UpdateHours: 12}
}

func newTestServer(core Core, logs io.Writer) http.Handler {
	if logs == nil {
		logs = io.Discard
	}
	return NewServer(core, testSettings(), logging.NewWithWriter("subs", "info", logs)).Handler()
}

func get(h http.Handler, target string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func stubName(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(rec.Body.String())
	if err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "vless://") || !strings.Contains(lines[0], "@127.0.0.1:1?") {
		t.Fatalf("want one stub link, got %q", raw)
	}
	name, err := url.PathUnescape(lines[0][strings.Index(lines[0], "#")+1:])
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestHealth(t *testing.T) {
	rec := get(newTestServer(&fakeCore{}, nil), "/health", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health = %d %s", rec.Code, rec.Body.String())
	}
}

func TestMalformedTokenIsNotFoundWithoutAskingCore(t *testing.T) {
	core := &fakeCore{view: okView()}
	h := newTestServer(core, nil)
	for _, bad := range []string{"abc", testToken[:31], testToken + "0", strings.ToUpper(testToken), "zz781f94d5ba0d5d8dd48fae364fa8da", "health2"} {
		if rec := get(h, "/"+bad, nil); rec.Code != http.StatusNotFound {
			t.Errorf("/%s = %d, want 404", bad, rec.Code)
		}
	}
	if rec := get(h, "/"+testToken+"/extra", nil); rec.Code != http.StatusNotFound {
		t.Errorf("nested path = %d", rec.Code)
	}
	if len(core.calls) != 0 {
		t.Fatalf("core was asked %d times", len(core.calls))
	}
}

func TestUnknownTokenIsNotFound(t *testing.T) {
	rec := get(newTestServer(&fakeCore{err: ErrNotFound}, nil), "/"+testToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d", rec.Code)
	}
	if rec.Header().Get("Subscription-Userinfo") != "" {
		t.Fatal("404 carries subscription headers")
	}
}

func TestCoreFailureIsServiceUnavailable(t *testing.T) {
	rec := get(newTestServer(&fakeCore{err: errors.New("dial tcp: connection refused")}, nil), "/"+testToken, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestBadRequestsNeverReachCore(t *testing.T) {
	core := &fakeCore{view: okView()}
	h := newTestServer(core, nil)
	if rec := get(h, "/"+testToken+"?format=json", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown format = %d", rec.Code)
	}
	if rec := get(h, "/"+testToken, map[string]string{"x-hwid": strings.Repeat("h", 257)}); rec.Code != http.StatusBadRequest {
		t.Errorf("oversized hwid = %d", rec.Code)
	}
	if len(core.calls) != 0 {
		t.Fatalf("core was asked %d times", len(core.calls))
	}
}

func TestClientHeadersReachCore(t *testing.T) {
	core := &fakeCore{view: okView()}
	get(newTestServer(core, nil), "/"+testToken, map[string]string{
		"User-Agent":     "Happ/3.6.1",
		"x-hwid":         "a1b2c3d4e5f6",
		"x-device-os":    "iOS",
		"x-ver-os":       "18.3",
		"x-device-model": "iPhone 14 Pro Max",
	})
	want := Request{SubToken: testToken, HWID: "a1b2c3d4e5f6", UserAgent: "Happ/3.6.1", DeviceOS: "iOS 18.3", DeviceModel: "iPhone 14 Pro Max"}
	if len(core.calls) != 1 || core.calls[0] != want {
		t.Fatalf("core got %+v, want %+v", core.calls, want)
	}
	get(newTestServer(core, nil), "/"+testToken, map[string]string{"x-device-os": "Android"})
	if core.calls[1].DeviceOS != "Android" || core.calls[1].HWID != "" {
		t.Fatalf("second call = %+v", core.calls[1])
	}
}

func TestOKServesConnectionsWithHeaders(t *testing.T) {
	rec := get(newTestServer(&fakeCore{view: okView()}, nil), "/"+testToken, map[string]string{"User-Agent": "Happ/3.6.1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), RenderBase64(goldenConnections())) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	want := map[string]string{
		"Content-Type":            "text/plain; charset=utf-8",
		"Subscription-Userinfo":   "upload=0; download=5368709120; total=107374182400; expire=1794378087",
		"Profile-Title":           "base64:" + base64.StdEncoding.EncodeToString([]byte("Sylph VPN")),
		"Profile-Update-Interval": "12",
		"Support-Url":             "https://t.me/sylph_support",
		"Cache-Control":           "no-store",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestFormatsFollowTheClient(t *testing.T) {
	h := newTestServer(&fakeCore{view: okView()}, nil)
	clash, _ := RenderClash(goldenConnections())
	rec := get(h, "/"+testToken, map[string]string{"User-Agent": "clash-verge/v2.4.2"})
	if rec.Header().Get("Content-Type") != "text/yaml; charset=utf-8" || !bytes.Equal(rec.Body.Bytes(), clash) {
		t.Fatalf("clash = %s %s", rec.Header().Get("Content-Type"), rec.Body.String())
	}
	rec = get(h, "/"+testToken+"?format=clash", map[string]string{"User-Agent": "Happ/3.6.1"})
	if !bytes.Equal(rec.Body.Bytes(), clash) {
		t.Fatalf("forced clash = %s", rec.Body.String())
	}
	rec = get(h, "/"+testToken+"?format=base64", map[string]string{"User-Agent": "mihomo/1.19.32"})
	if !bytes.Equal(rec.Body.Bytes(), RenderBase64(goldenConnections())) {
		t.Fatalf("forced base64 = %s", rec.Body.String())
	}
}

func TestSingBoxGetsUnsupportedStubEvenWhenOK(t *testing.T) {
	h := newTestServer(&fakeCore{view: okView()}, nil)
	for target, ua := range map[string]string{"/" + testToken: "SFA/1.14.3", "/" + testToken + "?format=singbox": "Happ/3.6.1"} {
		rec := get(h, target, map[string]string{"User-Agent": ua})
		body := rec.Body.String()
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("%s = %d %s", target, rec.Code, rec.Header().Get("Content-Type"))
		}
		if !strings.Contains(body, StubSingBox) || !strings.Contains(body, `"server": "127.0.0.1"`) || strings.Contains(body, "nl1.example.com") {
			t.Fatalf("%s served %s", target, body)
		}
		if rec.Header().Get("Subscription-Userinfo") == "" {
			t.Fatal("stub lost subscription headers")
		}
	}
}

func TestStubsExplainTheStatus(t *testing.T) {
	cases := map[string]string{
		StatusExpired:         "⛔ Подписка истекла — продли в боте",
		StatusTrafficExceeded: "⛔ Закончился трафик — продли в боте",
		StatusDeviceLimit:     "⛔ Лимит устройств — сбрось устройства в боте",
		StatusHWIDRequired:    "⛔ Клиент не поддерживается — нужна авторизация по HWID",
		"frozen":              "⛔ Доступ закрыт — напиши в поддержку",
	}
	for status, text := range cases {
		v := okView()
		v.Status = status
		rec := get(newTestServer(&fakeCore{view: v}, nil), "/"+testToken, map[string]string{"User-Agent": "Happ/3.6.1"})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: code %d", status, rec.Code)
		}
		if got := stubName(t, rec); got != text {
			t.Errorf("%s: stub %q, want %q", status, got, text)
		}
	}
}

func TestStubWhenNoUsableServers(t *testing.T) {
	empty := okView()
	empty.Connections = nil
	unknownKind := okView()
	unknownKind.Connections = []Connection{{Name: "WS", Address: "ws.example.com", Port: 443, Transport: "ws", Security: "tls"}}
	for _, v := range []*View{empty, unknownKind} {
		rec := get(newTestServer(&fakeCore{view: v}, nil), "/"+testToken, nil)
		if got := stubName(t, rec); got != StubNoServers {
			t.Errorf("stub %q, want %q", got, StubNoServers)
		}
	}
	mixed := okView()
	mixed.Connections = append(unknownKind.Connections, goldenConnections()...)
	rec := get(newTestServer(&fakeCore{view: mixed}, nil), "/"+testToken, nil)
	if !bytes.Equal(rec.Body.Bytes(), RenderBase64(goldenConnections())) {
		t.Fatalf("unknown kind leaked into the profile: %s", rec.Body.String())
	}
}

func TestUserinfoWithoutLimitOrSubscription(t *testing.T) {
	v := &View{Status: StatusExpired, TrafficUsedBytes: 0}
	rec := get(newTestServer(&fakeCore{view: v}, nil), "/"+testToken, nil)
	if got := rec.Header().Get("Subscription-Userinfo"); got != "upload=0; download=0; total=0; expire=0" {
		t.Fatalf("userinfo = %q", got)
	}
}

func TestSupportURLIsOptional(t *testing.T) {
	v := okView()
	v.SupportURL = ""
	s := NewServer(&fakeCore{view: v}, Settings{UpdateHours: 6}, logging.NewWithWriter("subs", "info", io.Discard))
	rec := get(s.Handler(), "/"+testToken, nil)
	if _, ok := rec.Header()["Support-Url"]; ok {
		t.Fatal("empty support url sent")
	}
	if rec.Header().Get("Profile-Update-Interval") != "6" {
		t.Fatalf("interval = %q", rec.Header().Get("Profile-Update-Interval"))
	}
}

func TestStubUUIDIsFreshEachTime(t *testing.T) {
	v := okView()
	v.Status = StatusExpired
	h := newTestServer(&fakeCore{view: v}, nil)
	a, b := get(h, "/"+testToken, nil).Body.String(), get(h, "/"+testToken, nil).Body.String()
	if a == b {
		t.Fatal("stub uuid repeats")
	}
}

func TestRequestIDIsOursAndReachesCore(t *testing.T) {
	core := &fakeCore{view: okView()}
	rec := get(newTestServer(core, nil), "/"+testToken, map[string]string{logging.HeaderRequestID: "from-the-internet"})
	id := rec.Header().Get(logging.HeaderRequestID)
	if id == "" || id == "from-the-internet" || core.ids[0] != id {
		t.Fatalf("response id %q, core saw %q", id, core.ids[0])
	}
}

func TestLogsCarryNoSecrets(t *testing.T) {
	var logs bytes.Buffer
	core := &fakeCore{view: okView()}
	h := newTestServer(core, &logs)
	hdr := map[string]string{"User-Agent": "Happ/3.6.1", "x-hwid": "secret-hwid-0001", "x-device-model": "Pixel"}
	get(h, "/"+testToken, hdr)
	get(h, "/"+testToken+"?format=clash", hdr)
	core.err = ErrNotFound
	get(h, "/"+testToken, hdr)
	core.err = errors.New("api 500 internal: boom")
	get(h, "/"+testToken, hdr)
	get(h, "/"+strings.ToUpper(testToken), hdr)
	out := logs.String()
	for _, secret := range []string{testToken, strings.ToUpper(testToken), "secret-hwid-0001", goldenConnections()[0].UUID, "Pixel"} {
		if strings.Contains(out, secret) {
			t.Fatalf("log leaks %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{`"msg":"subscription_request"`, `"format":"clash"`, `"client":"Happ"`, `"status":"ok"`, `"status":"not_found"`, `"status":"core_unavailable"`, `"status":"bad_token"`, `"request_id":"`} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %s:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "\n"); n != 5 {
		t.Errorf("want 5 log lines, got %d:\n%s", n, out)
	}
}

func TestSingBoxSeesTheRealReasonWhenNotOK(t *testing.T) {
	v := okView()
	v.Status = StatusExpired
	rec := get(newTestServer(&fakeCore{view: v}, nil), "/"+testToken, map[string]string{"User-Agent": "SFA/1.14.3"})
	if body := rec.Body.String(); !strings.Contains(body, StubExpired) || strings.Contains(body, StubSingBox) {
		t.Fatalf("sing-box with expired subscription got %s", body)
	}
}

func TestTrailingSlashServesTheSameSubscription(t *testing.T) {
	core := &fakeCore{view: okView()}
	rec := get(newTestServer(core, nil), "/"+testToken+"/", nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), RenderBase64(goldenConnections())) || core.calls[0].SubToken != testToken {
		t.Fatalf("trailing slash = %d %s %+v", rec.Code, rec.Body.String(), core.calls)
	}
}

func TestBrandComesFromCoreEachTime(t *testing.T) {
	core := &fakeCore{view: okView()}
	h := newTestServer(core, nil)
	core.view.Title, core.view.SupportURL = "Нора", "https://t.me/nora"
	rec := get(h, "/"+testToken, nil)
	if rec.Header().Get("Profile-Title") != "base64:"+base64.StdEncoding.EncodeToString([]byte("Нора")) || rec.Header().Get("Support-Url") != "https://t.me/nora" {
		t.Fatalf("headers = %v", rec.Header())
	}
	core.view.Title = ""
	rec = get(h, "/"+testToken, nil)
	if rec.Header().Get("Profile-Title") != "base64:"+base64.StdEncoding.EncodeToString([]byte("Sylph")) {
		t.Fatalf("title without core brand = %q", rec.Header().Get("Profile-Title"))
	}
	for _, bad := range []string{"javascript:alert(1)", "t.me/x", "https://x.example/a\r\nX-Evil: 1", "https://"} {
		core.view.SupportURL = bad
		rec = get(h, "/"+testToken, nil)
		if _, ok := rec.Header()["Support-Url"]; ok {
			t.Fatalf("support %q passed through", bad)
		}
		if page := browserGet(h, "sub.example.com", nil).Body.String(); strings.Contains(page, "Поддержка") {
			t.Fatalf("support %q shown on the page", bad)
		}
	}
}
