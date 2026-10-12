package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/yookassa"
)

const (
	fakeShopID = "424242"
	fakeSecret = "test_FakeShopSecret-must-not-leak"
)

type fakeYooKassa struct {
	mu       sync.Mutex
	payments map[string]map[string]any
	down     bool
	creates  int
}

func newFakeYooKassa(t *testing.T) (*fakeYooKassa, *yookassa.Client) {
	t.Helper()
	f := &fakeYooKassa{payments: map[string]map[string]any{}}
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	return f, yookassa.New(yookassa.Config{BaseURL: ts.URL, ShopID: fakeShopID, SecretKey: fakeSecret, Timeout: 2 * time.Second, RetryDelay: time.Millisecond})
}

func (f *fakeYooKassa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if user, pass, ok := r.BasicAuth(); !ok || user != fakeShopID || pass != fakeSecret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.down {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/payments":
		if r.Header.Get("Idempotence-Key") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.creates++
		id := model.NewUUID()
		p := map[string]any{
			"id": id, "status": "pending", "paid": false, "test": true,
			"amount": req["amount"], "metadata": req["metadata"], "description": req["description"],
			"confirmation": map[string]any{"type": "redirect", "confirmation_url": "https://yoomoney.example/checkout?orderId=" + id},
		}
		f.payments[id] = p
		json.NewEncoder(w).Encode(p)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/payments/"):
		p, ok := f.payments[strings.TrimPrefix(r.URL.Path, "/payments/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(p)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeYooKassa) setStatus(id, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payments[id]["status"] = status
	f.payments[id]["paid"] = status == "succeeded"
}

func (f *fakeYooKassa) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

func (f *fakeYooKassa) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates
}

func newPaymentsEnv(t *testing.T) (*apiEnv, *fakeYooKassa) {
	t.Helper()
	e := newAPIEnv(t)
	f, client := newFakeYooKassa(t)
	e.svc.EnablePayments(client, "https://t.me/sylph_bot")
	return e, f
}
