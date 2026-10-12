package bot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type notified struct {
	tg        int64
	paidUntil *time.Time
}

type webhookEnv struct {
	core   *fakeCore
	h      *Webhook
	notes  []notified
	logs   *bytes.Buffer
	notErr error
}

func newWebhookEnv(t *testing.T, trusted ...string) *webhookEnv {
	t.Helper()
	e := &webhookEnv{core: &fakeCore{}, logs: &bytes.Buffer{}}
	allow, err := prefixes("x", strings.Join(defaultYooKassaIPs, ","))
	if err != nil {
		t.Fatal(err)
	}
	proxies, err := prefixes("x", strings.Join(trusted, ","))
	if err != nil {
		t.Fatal(err)
	}
	e.h = NewWebhook(e.core, func(_ context.Context, tg int64, paidUntil *time.Time) error {
		e.notes = append(e.notes, notified{tg, paidUntil})
		return e.notErr
	}, allow, proxies, slog.New(slog.NewJSONHandler(e.logs, nil)))
	return e
}

const paymentID = "2d85d5ad-000f-5000-9000-1c4c6dd1d9e6"

func notification(event, id string) string {
	return `{"type":"notification","event":"` + event + `","object":{"id":"` + id + `","status":"succeeded","amount":{"value":"1.00","currency":"RUB"},"metadata":{"user_id":"1","plan_id":"1"}}}`
}

func (e *webhookEnv) post(remote, xff, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/yookassa", strings.NewReader(body))
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func TestWebhookFromYooKassaChecksThePaymentAndNotifies(t *testing.T) {
	e := newWebhookEnv(t)
	e.core.check = &Check{Status: "succeeded", Applied: true, TelegramID: i64(1001), PaidUntil: tptr(at("2026-11-30T10:00:00Z"))}
	w := e.post("185.71.76.10:5555", "", notification("payment.succeeded", paymentID))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !e.core.called("check:" + paymentID) {
		t.Fatalf("core calls = %v", e.core.calls)
	}
	if len(e.notes) != 1 || e.notes[0].tg != 1001 || e.notes[0].paidUntil == nil {
		t.Fatalf("notes = %+v", e.notes)
	}
}

func TestWebhookTrustsNothingButThePaymentID(t *testing.T) {
	e := newWebhookEnv(t)
	e.core.check = &Check{Status: "pending", TelegramID: i64(1001)}
	if w := e.post("185.71.76.10:5555", "", notification("payment.succeeded", paymentID)); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if len(e.notes) != 0 {
		t.Fatal("notified although the core did not apply the payment")
	}
	e.core.check = &Check{Status: "succeeded", Applied: false, TelegramID: i64(1001)}
	e.post("185.71.76.10:5555", "", notification("payment.succeeded", paymentID))
	if len(e.notes) != 0 {
		t.Fatal("notified twice for one payment")
	}
}

func TestWebhookRejectsStrangers(t *testing.T) {
	e := newWebhookEnv(t)
	for _, remote := range []string{"8.8.8.8:1", "185.71.76.32:1", "[2a02:5181::1]:1", "garbage"} {
		if w := e.post(remote, "", notification("payment.succeeded", paymentID)); w.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d", remote, w.Code)
		}
	}
	if w := e.post("8.8.8.8:1", "185.71.76.10", notification("payment.succeeded", paymentID)); w.Code != http.StatusForbidden {
		t.Fatalf("forwarded header from an untrusted peer was believed: %d", w.Code)
	}
	if len(e.core.calls) != 0 {
		t.Fatalf("core called for strangers: %v", e.core.calls)
	}
	if !strings.Contains(e.logs.String(), `"msg":"webhook_rejected"`) {
		t.Fatalf("no webhook_rejected log: %s", e.logs.String())
	}
}

func TestWebhookBehindTheProxy(t *testing.T) {
	e := newWebhookEnv(t, "172.18.0.0/16")
	e.core.check = &Check{Status: "pending", TelegramID: i64(1001)}
	if w := e.post("172.18.0.5:4000", "185.71.76.10", notification("payment.succeeded", paymentID)); w.Code != http.StatusOK {
		t.Fatalf("yookassa via proxy: %d", w.Code)
	}
	if w := e.post("172.18.0.5:4000", "185.71.76.10, 8.8.8.8", notification("payment.succeeded", paymentID)); w.Code != http.StatusForbidden {
		t.Fatalf("stranger appended after yookassa got in: %d", w.Code)
	}
	if w := e.post("172.18.0.5:4000", "8.8.8.8, 185.71.76.10", notification("payment.succeeded", paymentID)); w.Code != http.StatusOK {
		t.Fatalf("spoofed left part must not matter, rightmost hop is yookassa: %d", w.Code)
	}
	if w := e.post("172.18.0.5:4000", "185.71.76.10, 172.18.0.9", notification("payment.succeeded", paymentID)); w.Code != http.StatusOK {
		t.Fatalf("second trusted hop must be skipped: %d", w.Code)
	}
	if w := e.post("172.18.0.5:4000", "", notification("payment.succeeded", paymentID)); w.Code != http.StatusForbidden {
		t.Fatalf("proxy itself is not yookassa: %d", w.Code)
	}
	if w := e.post("172.18.0.5:4000", "not-an-ip", notification("payment.succeeded", paymentID)); w.Code != http.StatusForbidden {
		t.Fatalf("garbage forwarded header: %d", w.Code)
	}
}

func TestWebhookBadRequests(t *testing.T) {
	e := newWebhookEnv(t)
	yk := "185.71.76.10:1"
	for name, body := range map[string]string{
		"not json":   "{",
		"no id":      `{"type":"notification","event":"payment.succeeded","object":{}}`,
		"long id":    notification("payment.succeeded", strings.Repeat("a", 65)),
		"slashes":    notification("payment.succeeded", "../users/1/rotate"),
		"dots":       notification("payment.succeeded", ".."),
		"two values": notification("payment.succeeded", paymentID) + "{}",
	} {
		if w := e.post(yk, "", body); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", name, w.Code)
		}
	}
	big := `{"type":"notification","event":"payment.succeeded","object":{"id":"` + paymentID + `","pad":"` + strings.Repeat("x", 70<<10) + `"}}`
	if w := e.post(yk, "", big); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big body: status = %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/yookassa", nil)
	r.RemoteAddr = yk
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: status = %d", w.Code)
	}
	if len(e.core.calls) != 0 {
		t.Fatalf("core called for bad requests: %v", e.core.calls)
	}
}

func TestWebhookIgnoresOtherEvents(t *testing.T) {
	e := newWebhookEnv(t)
	if w := e.post("185.71.76.10:1", "", notification("refund.succeeded", paymentID)); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if len(e.core.calls) != 0 {
		t.Fatalf("refund triggered a payment check: %v", e.core.calls)
	}
}

func TestWebhookAsksYooKassaToRetryWhenTheCoreIsDown(t *testing.T) {
	e := newWebhookEnv(t)
	e.core.checkErr = errors.New("core down")
	if w := e.post("185.71.76.10:1", "", notification("payment.succeeded", paymentID)); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", w.Code)
	}
	e.core.checkErr = ErrProviderUnavailable
	if w := e.post("185.71.76.10:1", "", notification("payment.succeeded", paymentID)); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("provider down: status = %d", w.Code)
	}
	e.core.checkErr = ErrNotFound
	if w := e.post("185.71.76.10:1", "", notification("payment.succeeded", paymentID)); w.Code != http.StatusOK {
		t.Fatalf("unknown payment must not be retried: %d", w.Code)
	}
}

func TestWebhookNotifyFailureStillAnswers200(t *testing.T) {
	e := newWebhookEnv(t)
	e.notErr = errors.New("telegram down")
	e.core.check = &Check{Status: "succeeded", Applied: true, TelegramID: i64(1001)}
	if w := e.post("185.71.76.10:1", "", notification("payment.succeeded", paymentID)); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(e.logs.String(), `"msg":"payment_notify_failed"`) {
		t.Fatalf("logs = %s", e.logs.String())
	}
}

func TestClientIPUnmapsIPv4InIPv6(t *testing.T) {
	e := newWebhookEnv(t)
	e.core.check = &Check{Status: "pending", TelegramID: i64(1)}
	if w := e.post("[::ffff:185.71.76.10]:1", "", notification("payment.succeeded", paymentID)); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}
