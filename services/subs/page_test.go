package subs

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var browser = map[string]string{
	"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 18_2 like Mac OS X) AppleWebKit/605.1.15 Mobile Safari/604.1",
	"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
}

func browserGet(h http.Handler, host string, extra map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/"+testToken, nil)
	req.Host = host
	for k, v := range browser {
		req.Header.Set(k, v)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBrowserGetsAPage(t *testing.T) {
	core := &fakeCore{view: okView()}
	rec := browserGet(newTestServer(core, nil), "sub.example.com", map[string]string{"X-Forwarded-Proto": "https"})
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("page = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	link := "https://sub.example.com/" + testToken
	for _, want := range []string{
		"Sylph VPN", "Подписка активна", "5 ГБ", "100 ГБ", "Оплачено до", "11 ноября 2026, 09:21 MSK",
		`href="happ://add/` + link + `"`, `href="v2raytun://import/` + link + `"`,
		"https://apps.apple.com/ru/app/happ-proxy-utility-plus/id6746188973", "https://play.google.com/store/apps/details?id=com.happproxy",
		"https://apps.apple.com/app/id6476628951", "https://play.google.com/store/apps/details?id=com.v2raytun.android",
		`value="` + link + `"`, "Скопировать ссылку", "https://t.me/sylph_support",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "vless://") || strings.Contains(body, goldenConnections()[0].UUID) {
		t.Fatal("page leaks connections")
	}
	h := rec.Header()
	if h.Get("Referrer-Policy") != "no-referrer" || h.Get("Cache-Control") != "no-store" || !strings.Contains(h.Get("X-Robots-Tag"), "noindex") || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", h)
	}
	csp := h.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("csp = %q", csp)
	}
	for _, tag := range []string{"script", "style"} {
		inline := between(t, body, "<"+tag+">", "</"+tag+">")
		sum := sha256.Sum256([]byte(inline))
		if !strings.Contains(csp, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
			t.Fatalf("csp %q does not allow the inline %s", csp, tag)
		}
	}
	if len(core.calls) != 1 || core.calls[0].HWID != "" {
		t.Fatalf("browser must not take a device slot: %+v", core.calls)
	}
}

func between(t *testing.T, s, open, close string) string {
	t.Helper()
	i := strings.Index(s, open)
	j := strings.Index(s, close)
	if i < 0 || j < i {
		t.Fatalf("no %s in page", open)
	}
	return s[i+len(open) : j]
}

func TestClientsStillGetProfiles(t *testing.T) {
	for name, hdr := range map[string]map[string]string{
		"mozilla without html":   {"User-Agent": "Mozilla/5.0", "Accept": "*/*"},
		"happ asking for html":   {"User-Agent": "Happ/1.6.0", "Accept": "text/html"},
		"forced format":          browser,
		"browser-like with hwid": {"User-Agent": browser["User-Agent"], "Accept": browser["Accept"], "X-Hwid": "hw-1"},
	} {
		t.Run(name, func(t *testing.T) {
			target := "/" + testToken
			if name == "forced format" {
				target += "?format=base64"
			}
			rec := get(newTestServer(&fakeCore{view: okView()}, nil), target, hdr)
			if rec.Code != http.StatusOK || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestPageExplainsTheStatus(t *testing.T) {
	for status, want := range map[string][]string{
		StatusExpired:         {"Подписка истекла"},
		StatusTrafficExceeded: {"Закончился трафик"},
		StatusHWIDRequired:    {"Подписка активна", "Happ или v2RayTun"},
		"weird":               {"Доступ закрыт"},
	} {
		v := okView()
		v.Status = status
		rec := browserGet(newTestServer(&fakeCore{view: v}, nil), "sub.example.com", nil)
		for _, w := range want {
			if !strings.Contains(rec.Body.String(), w) {
				t.Fatalf("%s: page lacks %q", status, w)
			}
		}
	}
	v := okView()
	v.TrafficLimitBytes, v.ExpiresAt = nil, nil
	body := browserGet(newTestServer(&fakeCore{view: v}, nil), "sub.example.com", nil).Body.String()
	if !strings.Contains(body, "без лимита") || strings.Contains(body, "Оплачено до") {
		t.Fatalf("unlimited page:\n%s", body)
	}
}

func TestPageWithAForgedHostHasNoClientButtons(t *testing.T) {
	rec := browserGet(newTestServer(&fakeCore{view: okView()}, nil), `evil.com"><script>alert(1)</script>`, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Contains(body, "happ://") || strings.Contains(body, "alert(1)") {
		t.Fatalf("forged host page:\n%s", body)
	}
	plain := browserGet(newTestServer(&fakeCore{view: okView()}, nil), "localhost:8080", nil).Body.String()
	if !strings.Contains(plain, `href="happ://add/http://localhost:8080/`+testToken+`"`) {
		t.Fatalf("plain http host:\n%s", plain)
	}
}

func TestPageIsLoggedWithoutTheToken(t *testing.T) {
	var logs bytes.Buffer
	browserGet(newTestServer(&fakeCore{view: okView()}, &logs), "sub.example.com", nil)
	if !strings.Contains(logs.String(), `"format":"html"`) || strings.Contains(logs.String(), testToken) {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestTrialUsedStub(t *testing.T) {
	v := okView()
	v.Status = StatusTrialUsed
	rec := get(newTestServer(&fakeCore{view: v}, nil), "/"+testToken, map[string]string{"User-Agent": "Happ/1.6.0"})
	if got := stubName(t, rec); got != StubTrialUsed {
		t.Fatalf("stub = %q", got)
	}
}
