package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

var _ XrayClient = newFakeXray()

func TestFakeXrayUserLifecycle(t *testing.T) {
	fx := newFakeXray()
	ctx := context.Background()
	if err := fx.AddUser(ctx, "vless-in", "u-1", userEmail("u-1", "vless-in"), "xtls-rprx-vision"); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("vless-in", "u-1") || fx.flowOf("vless-in", "u-1") != "xtls-rprx-vision" {
		t.Fatal("user not added with its flow")
	}
	if err := fx.RemoveUser(ctx, "vless-in", userEmail("u-1", "vless-in")); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("vless-in", "u-1") {
		t.Fatal("user not removed")
	}
}

func TestWaitForXrayReadyImmediately(t *testing.T) {
	fx := newFakeXray()
	fx.set(func(f *fakeXray) { f.uptime = 5 * time.Second })
	var buf bytes.Buffer
	if !WaitForXray(context.Background(), fx, time.Second, time.Millisecond, slog.New(slog.NewJSONHandler(&buf, nil))) {
		t.Fatal("want ready")
	}
	if n := fx.uptimeCalls(); n != 1 {
		t.Fatalf("uptime calls = %d, want 1", n)
	}
	if !strings.Contains(buf.String(), `"msg":"xray_ready"`) || !strings.Contains(buf.String(), `"xray_uptime_sec":5`) {
		t.Fatalf("log = %s", buf.String())
	}
}

func TestWaitForXrayRetriesUntilReady(t *testing.T) {
	fx := newFakeXray()
	fx.set(func(f *fakeXray) { f.uptimeFail = 3 })
	if !WaitForXray(context.Background(), fx, 5*time.Second, time.Millisecond, discardLogger()) {
		t.Fatal("want ready after retries")
	}
	if n := fx.uptimeCalls(); n != 4 {
		t.Fatalf("uptime calls = %d, want 4", n)
	}
}

func TestWaitForXrayGivesUpLoudly(t *testing.T) {
	fx := newFakeXray()
	fx.set(func(f *fakeXray) { f.uptimeErr = errors.New("connection refused") })
	var buf bytes.Buffer
	start := time.Now()
	if WaitForXray(context.Background(), fx, 50*time.Millisecond, 10*time.Millisecond, slog.New(slog.NewJSONHandler(&buf, nil))) {
		t.Fatal("want not ready")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waited %v, want about 50ms", elapsed)
	}
	if fx.uptimeCalls() < 2 {
		t.Fatalf("uptime calls = %d, want retries", fx.uptimeCalls())
	}
	if !strings.Contains(buf.String(), `"msg":"xray_not_ready"`) || !strings.Contains(buf.String(), "connection refused") {
		t.Fatalf("log = %s", buf.String())
	}
}

func TestWaitForXrayStopsOnShutdown(t *testing.T) {
	fx := newFakeXray()
	fx.set(func(f *fakeXray) { f.uptimeErr = errors.New("connection refused") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	start := time.Now()
	if WaitForXray(ctx, fx, 10*time.Second, 10*time.Millisecond, slog.New(slog.NewJSONHandler(&buf, nil))) {
		t.Fatal("want false on shutdown")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("shutdown took %v", elapsed)
	}
	if strings.Contains(buf.String(), "xray_not_ready") {
		t.Fatalf("shutdown is not an xray failure: %s", buf.String())
	}
}
