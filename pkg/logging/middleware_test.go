package logging

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareGeneratesRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter("test", "info", &buf)
	var ctxID string
	h := Middleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if ctxID == "" {
		t.Fatal("request id missing in context")
	}
	if len(ctxID) != 36 {
		t.Fatalf("generated id len = %d, want 36", len(ctxID))
	}
	if rec.Header().Get(HeaderRequestID) != ctxID {
		t.Fatalf("response header = %q, want %q", rec.Header().Get(HeaderRequestID), ctxID)
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &m); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if m["msg"] != "http_request" {
		t.Fatalf("msg = %v, want http_request", m["msg"])
	}
	if m["request_id"] != ctxID {
		t.Fatalf("log request_id = %v, want %v", m["request_id"], ctxID)
	}
	if m["status"].(float64) != float64(http.StatusTeapot) {
		t.Fatalf("log status = %v, want %d", m["status"], http.StatusTeapot)
	}
}

func TestMiddlewareReusesIncomingRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter("test", "info", &buf)
	var ctxID string
	h := Middleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, "incoming-id")
	h.ServeHTTP(rec, req)
	if ctxID != "incoming-id" {
		t.Fatalf("ctx id = %q, want incoming-id", ctxID)
	}
	if rec.Header().Get(HeaderRequestID) != "incoming-id" {
		t.Fatalf("response header = %q, want incoming-id", rec.Header().Get(HeaderRequestID))
	}
}

func TestMiddlewareReplacesOversizedRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter("test", "info", &buf)
	var ctxID string
	h := Middleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromContext(r.Context())
	}))
	oversized := strings.Repeat("x", 200)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, oversized)
	h.ServeHTTP(rec, req)
	if ctxID == oversized {
		t.Fatal("oversized request id should not pass through")
	}
	if len(ctxID) != 36 {
		t.Fatalf("replaced id len = %d, want 36", len(ctxID))
	}
	if rec.Header().Get(HeaderRequestID) != ctxID {
		t.Fatalf("response header = %q, want %q", rec.Header().Get(HeaderRequestID), ctxID)
	}
}

func TestMiddlewarePanicPropagates(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter("test", "info", &buf)
	h := Middleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	defer func() {
		if recover() == nil {
			t.Fatal("panic should propagate")
		}
		if !strings.Contains(buf.String(), "http_panic") {
			t.Fatalf("panic log missing: %q", buf.String())
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))
	t.Fatal("unreachable")
}
