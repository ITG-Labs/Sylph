package yookassa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testShop   = "123456"
	testSecret = "test_SuperSecretKey-abc"
)

type recorded struct {
	method, path, raw, key string
	user, pass             string
	authOK                 bool
	body                   map[string]any
}

type fakeAPI struct {
	mu       sync.Mutex
	calls    []recorded
	statuses []int
	reply    string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := recorded{method: r.Method, path: r.URL.Path, raw: r.URL.EscapedPath(), key: r.Header.Get("Idempotence-Key")}
	rec.user, rec.pass, rec.authOK = r.BasicAuth()
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&rec.body)
	}
	f.calls = append(f.calls, rec)
	status := http.StatusOK
	if len(f.statuses) > 0 {
		status, f.statuses = f.statuses[0], f.statuses[1:]
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status == http.StatusOK {
		w.Write([]byte(f.reply))
		return
	}
	w.Write([]byte(`{"type":"error","code":"invalid_request","description":"bad things"}`))
}

func newTestClient(t *testing.T, f *fakeAPI) *Client {
	t.Helper()
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	return New(Config{BaseURL: ts.URL, ShopID: testShop, SecretKey: testSecret, Timeout: 2 * time.Second, RetryDelay: time.Millisecond})
}

const createdReply = `{"id":"2d85d5ad-000f-5000-9000-1c4c6dd1d9e6","status":"pending","paid":false,
 "amount":{"value":"199.00","currency":"RUB"},
 "confirmation":{"type":"redirect","confirmation_url":"https://yoomoney.ru/checkout/payments/v2/contract?orderId=2d85d5ad"},
 "metadata":{"user_id":"7","plan_id":"3"},"test":true,"created_at":"2026-10-12T10:00:00.000Z"}`

func TestCreatePaymentSendsV3Request(t *testing.T) {
	f := &fakeAPI{reply: createdReply}
	c := newTestClient(t, f)
	p, err := c.CreatePayment(context.Background(), CreateRequest{
		Amount:       RUB(199),
		Capture:      true,
		Confirmation: Confirmation{Type: "redirect", ReturnURL: "https://t.me/sylph_bot"},
		Description:  "Месяц",
		Metadata:     map[string]string{"user_id": "7", "plan_id": "3"},
	}, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "2d85d5ad-000f-5000-9000-1c4c6dd1d9e6" || p.Status != "pending" || p.Confirmation == nil ||
		!strings.HasPrefix(p.Confirmation.ConfirmationURL, "https://yoomoney.ru/") || p.Metadata["user_id"] != "7" || !p.Test {
		t.Fatalf("payment = %+v", p)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %d", len(f.calls))
	}
	got := f.calls[0]
	if got.method != http.MethodPost || got.path != "/payments" || got.key != "key-1" {
		t.Fatalf("request = %s %s key %q", got.method, got.path, got.key)
	}
	if !got.authOK || got.user != testShop || got.pass != testSecret {
		t.Fatalf("basic auth = %q / %q (%v)", got.user, got.pass, got.authOK)
	}
	amount, _ := got.body["amount"].(map[string]any)
	conf, _ := got.body["confirmation"].(map[string]any)
	meta, _ := got.body["metadata"].(map[string]any)
	if amount["value"] != "199.00" || amount["currency"] != "RUB" || got.body["capture"] != true ||
		conf["type"] != "redirect" || conf["return_url"] != "https://t.me/sylph_bot" ||
		got.body["description"] != "Месяц" || meta["user_id"] != "7" || meta["plan_id"] != "3" {
		t.Fatalf("body = %+v", got.body)
	}
	if _, ok := got.body["receipt"]; ok {
		t.Fatal("receipt must not be sent")
	}
}

func TestCreatePaymentRetriesServerErrorsWithTheSameKey(t *testing.T) {
	f := &fakeAPI{reply: createdReply, statuses: []int{http.StatusInternalServerError, http.StatusOK}}
	c := newTestClient(t, f)
	if _, err := c.CreatePayment(context.Background(), CreateRequest{Amount: RUB(1)}, "key-retry"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || f.calls[0].key != "key-retry" || f.calls[1].key != "key-retry" {
		t.Fatalf("calls = %+v", f.calls)
	}
}

func TestCreatePaymentGivesUpAfterOneRetry(t *testing.T) {
	f := &fakeAPI{statuses: []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusOK}, reply: createdReply}
	c := newTestClient(t, f)
	_, err := c.CreatePayment(context.Background(), CreateRequest{Amount: RUB(1)}, "k")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadGateway {
		t.Fatalf("err = %v", err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(f.calls))
	}
}

func TestCreatePaymentDoesNotRetryClientErrors(t *testing.T) {
	f := &fakeAPI{statuses: []int{http.StatusBadRequest}}
	c := newTestClient(t, f)
	_, err := c.CreatePayment(context.Background(), CreateRequest{Amount: RUB(1)}, "k")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || apiErr.Code != "invalid_request" || apiErr.Description != "bad things" {
		t.Fatalf("err = %v", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(f.calls))
	}
}

func TestGetPayment(t *testing.T) {
	f := &fakeAPI{reply: `{"id":"2d85d5ad-000f-5000-9000-1c4c6dd1d9e6","status":"canceled","paid":false,
	 "amount":{"value":"199.00","currency":"RUB"},"metadata":{"user_id":"7","plan_id":null},
	 "cancellation_details":{"party":"yoo_money","reason":"expired_on_confirmation"}}`}
	c := newTestClient(t, f)
	p, err := c.GetPayment(context.Background(), "2d85d5ad-000f-5000-9000-1c4c6dd1d9e6")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "canceled" || p.CancellationDetails == nil || p.CancellationDetails.Reason != "expired_on_confirmation" || p.Metadata["plan_id"] != "" {
		t.Fatalf("payment = %+v", p)
	}
	got := f.calls[0]
	if got.method != http.MethodGet || got.path != "/payments/2d85d5ad-000f-5000-9000-1c4c6dd1d9e6" || !got.authOK || got.user != testShop || got.pass != testSecret {
		t.Fatalf("request = %+v", got)
	}
}

func TestGetPaymentNotFound(t *testing.T) {
	f := &fakeAPI{statuses: []int{http.StatusNotFound}}
	c := newTestClient(t, f)
	if _, err := c.GetPayment(context.Background(), "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("404 retried: %d calls", len(f.calls))
	}
}

func TestGetPaymentEscapesTheID(t *testing.T) {
	f := &fakeAPI{reply: `{"id":"a","status":"pending","amount":{"value":"1.00","currency":"RUB"}}`}
	c := newTestClient(t, f)
	if _, err := c.GetPayment(context.Background(), "../me"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].raw != "/payments/..%2Fme" {
		t.Fatalf("id is not escaped: %q", f.calls[0].raw)
	}
}

func TestErrorsDoNotCarryTheSecret(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1", ShopID: testShop, SecretKey: testSecret, Timeout: 200 * time.Millisecond, RetryDelay: time.Millisecond})
	_, err := c.GetPayment(context.Background(), "x")
	if err == nil {
		t.Fatal("unreachable API must fail")
	}
	if strings.Contains(err.Error(), testSecret) {
		t.Fatalf("error leaks the secret: %v", err)
	}
}

func TestAmountKopecks(t *testing.T) {
	for value, want := range map[string]int64{"199.00": 19900, "0.01": 1, "1000.50": 100050} {
		got, ok := Amount{Value: value, Currency: "RUB"}.Kopecks()
		if !ok || got != want {
			t.Fatalf("%q = %d, %v; want %d", value, got, ok, want)
		}
	}
	for _, value := range []string{"199", "199.0", "199.000", "-1.00", "1e3.00", "", "abc.de", " 199.00", "199.0O"} {
		if _, ok := (Amount{Value: value}).Kopecks(); ok {
			t.Fatalf("%q must not parse", value)
		}
	}
	if RUB(199) != (Amount{Value: "199.00", Currency: "RUB"}) {
		t.Fatalf("RUB(199) = %+v", RUB(199))
	}
}

func TestNetworkErrorIsRetriedWithTheSameKey(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		keys = append(keys, r.Header.Get("Idempotence-Key"))
		mu.Unlock()
		if n == 1 {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.Write([]byte(createdReply))
	}))
	t.Cleanup(ts.Close)
	c := New(Config{BaseURL: ts.URL, ShopID: testShop, SecretKey: testSecret, Timeout: 2 * time.Second, RetryDelay: time.Millisecond})
	if _, err := c.CreatePayment(context.Background(), CreateRequest{Amount: RUB(1)}, "key-net"); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != "key-net" || keys[1] != "key-net" {
		t.Fatalf("keys = %v", keys)
	}
}
