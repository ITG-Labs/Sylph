package xraygrpc

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/agent/internal/xraytest"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	c, err := Dial(xraytest.API(t), "", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestLiveUserLifecycle(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	u := xraytest.NewUUID()
	if err := c.AddUser(ctx, xraytest.ClientTag, u); err != nil {
		t.Fatal(err)
	}
	if err := c.AddUser(ctx, xraytest.ClientTag, u); err != nil {
		t.Fatalf("second add must be idempotent: %v", err)
	}
	list, err := c.ListUsers(ctx, xraytest.ClientTag)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(list, u) {
		t.Fatal("added user missing from xray")
	}
	if err := c.RemoveUser(ctx, xraytest.ClientTag, u); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveUser(ctx, xraytest.ClientTag, u); err != nil {
		t.Fatalf("second remove must be idempotent: %v", err)
	}
	list, err = c.ListUsers(ctx, xraytest.ClientTag)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(list, u) {
		t.Fatal("removed user still in xray")
	}
}

func TestLiveUnknownInbound(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	u := xraytest.NewUUID()
	if err := c.AddUser(ctx, "sylph-missing", u); err == nil || !strings.Contains(err.Error(), "handler not found") {
		t.Fatalf("add: err = %v, want handler not found", err)
	}
	if err := c.RemoveUser(ctx, "sylph-missing", u); err == nil || !strings.Contains(err.Error(), "handler not found") {
		t.Fatalf("remove: err = %v, want handler not found", err)
	}
	if _, err := c.ListUsers(ctx, "sylph-missing"); err == nil {
		t.Fatal("list: want error")
	}
}

func TestLiveTrafficCounters(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	u := xraytest.NewUUID()
	if err := c.AddUser(ctx, xraytest.ClientTag, u); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.RemoveUser(context.Background(), xraytest.ClientTag, u) })
	if err := xraytest.Exchange(xraytest.ClientVLESS, u, []byte(strings.Repeat("p", 100)), 1000); err != nil {
		t.Fatal(err)
	}
	xraytest.WaitTraffic(t, c.QueryStats, u, 100, 1000)
}

func TestLiveRemovedUserCannotConnect(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	u := xraytest.NewUUID()
	if err := c.AddUser(ctx, xraytest.ClientTag, u); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveUser(ctx, xraytest.ClientTag, u); err != nil {
		t.Fatal(err)
	}
	if err := xraytest.Exchange(xraytest.ClientVLESS, u, []byte("hello"), 10); err == nil {
		t.Fatal("removed user still gets traffic through")
	}
}

func TestLiveUptime(t *testing.T) {
	c := liveClient(t)
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		up, err := c.Uptime(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if up > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("uptime stays zero")
		}
	}
}

func TestDialIsLazyAndCallsFailOnDeadPort(t *testing.T) {
	c, err := Dial("127.0.0.1:1", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Uptime(context.Background()); err == nil {
		t.Fatal("want error from closed port")
	}
}
