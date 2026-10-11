package xraygrpc

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/statscmd"
)

func TestSeesXrayShortlyAfterLongOutage(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()
	c, err := Dial(addr, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Uptime(context.Background()); err == nil {
		t.Fatal("xray is not up yet, uptime must fail")
	}
	time.Sleep(11400 * time.Millisecond)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	statscmd.RegisterStatsServiceServer(srv, &fakeXray{uptime: 7})
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()
	up := time.Now()
	for {
		if _, err := c.Uptime(context.Background()); err == nil {
			break
		}
		if time.Since(up) > 5*time.Second {
			t.Fatal("xray never seen after it came back")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if seen := time.Since(up); seen > 900*time.Millisecond {
		t.Fatalf("xray seen %v after it came back, want under 900ms", seen)
	}
}
