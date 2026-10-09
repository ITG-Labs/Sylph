package apiclient

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

func newTestClient(t *testing.T, baseURL string, buf *bytes.Buffer) *Client {
	t.Helper()
	c, err := New("test", baseURL, nil, logging.NewWithWriter("test", "info", buf))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDoPropagatesRequestID(t *testing.T) {
	var buf bytes.Buffer
	var gotRID, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRID = r.Header.Get(logging.HeaderRequestID)
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL, &buf)
	ctx := logging.WithRequestID(context.Background(), "rid-42")
	if err := c.Do(ctx, http.MethodGet, "/v1/users", nil, nil); err != nil {
		t.Fatal(err)
	}
	if gotRID != "rid-42" {
		t.Fatalf("request id = %q, want rid-42", gotRID)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/users" {
		t.Fatalf("path = %q, want /v1/users", gotPath)
	}
	if !strings.Contains(buf.String(), "api_request") {
		t.Fatalf("access log missing: %q", buf.String())
	}
}

func TestDoGeneratesRequestIDWhenMissing(t *testing.T) {
	var buf bytes.Buffer
	var gotRID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRID = r.Header.Get(logging.HeaderRequestID)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL, &buf)
	if err := c.Do(context.Background(), http.MethodGet, "/v1/users", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(gotRID) != 36 {
		t.Fatalf("generated request id len = %d, want 36", len(gotRID))
	}
}

func TestDoAPIError(t *testing.T) {
	var buf bytes.Buffer
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"code":"conflict","message":"user exists"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL, &buf)
	err := c.Do(context.Background(), http.MethodPost, "/v1/users", map[string]string{"x": "y"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", apiErr.StatusCode)
	}
	if apiErr.Code != "conflict" {
		t.Fatalf("code = %q, want conflict", apiErr.Code)
	}
	if apiErr.Message != "user exists" {
		t.Fatalf("message = %q, want user exists", apiErr.Message)
	}
	if apiErr.RequestID == "" {
		t.Fatal("request id should be filled")
	}
}

func TestHealth(t *testing.T) {
	var buf bytes.Buffer
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","version":"1.2.3"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL, &buf)
	hr, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hr.Status != "ok" || hr.Version != "1.2.3" {
		t.Fatalf("health = %+v", hr)
	}
}

func TestHealthNetworkError(t *testing.T) {
	var buf bytes.Buffer
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := newTestClient(t, url, &buf)
	_, err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("network error should not be *APIError: %v", err)
	}
}
