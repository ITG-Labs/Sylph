package subs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/apiclient"
	"github.com/ITG-Labs/sylph/pkg/logging"
)

func coreClientFor(t *testing.T, h http.HandlerFunc) *CoreClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	api, err := apiclient.New("subs", srv.URL, nil, logging.NewWithWriter("subs", "error", io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return NewCoreClient(api)
}

func TestCoreClientPostsTheRequestAndDecodesTheView(t *testing.T) {
	var got Request
	var gotID string
	c := coreClientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/subscription" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		gotID = r.Header.Get(logging.HeaderRequestID)
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"status":"ok","traffic_used_bytes":7,"traffic_limit_bytes":null,"expires_at":"2026-11-11T09:21:27+03:00",
			"connections":[{"name":"Смоук","address":"node.example.com","port":443,"uuid":"u","flow":"xtls-rprx-vision","transport":"tcp",
			"security":"reality","sni":"www.apple.com","public_key":"pk","short_id":"sid","fingerprint":"chrome"}]}`)
	})
	req := Request{SubToken: testToken, HWID: "h", UserAgent: "Happ/3.6.1", DeviceOS: "iOS 18.3", DeviceModel: "iPhone"}
	view, err := c.Subscription(logging.WithRequestID(context.Background(), "rid-1"), req)
	if err != nil {
		t.Fatal(err)
	}
	if got != req || gotID != "rid-1" {
		t.Fatalf("core got %+v id %q", got, gotID)
	}
	want := Connection{Name: "Смоук", Address: "node.example.com", Port: 443, UUID: "u", Flow: "xtls-rprx-vision", Transport: "tcp", Security: "reality", SNI: "www.apple.com", PublicKey: "pk", ShortID: "sid", Fingerprint: "chrome"}
	if view.Status != StatusOK || view.TrafficUsedBytes != 7 || view.TrafficLimitBytes != nil || view.ExpiresAt == nil || len(view.Connections) != 1 || view.Connections[0] != want {
		t.Fatalf("view = %+v", view)
	}
}

func TestCoreClientMapsErrors(t *testing.T) {
	notFound := coreClientFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":"not_found","message":"subscription not found"}`)
	})
	if _, err := notFound.Subscription(context.Background(), Request{SubToken: testToken}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("404 = %v", err)
	}
	for _, code := range []int{http.StatusInternalServerError, http.StatusForbidden, http.StatusBadRequest} {
		c := coreClientFor(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		_, err := c.Subscription(context.Background(), Request{SubToken: testToken})
		if err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("%d = %v", code, err)
		}
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	api, _ := apiclient.New("subs", srv.URL, nil, logging.NewWithWriter("subs", "error", io.Discard))
	if _, err := NewCoreClient(api).Subscription(context.Background(), Request{SubToken: testToken}); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("core down = %v", err)
	}
}
