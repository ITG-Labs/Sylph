package xraytest

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

const (
	EngineTag   = "vless-in"
	EngineVLESS = "127.0.0.1:55443"
	ClientTag   = "client-in"
	ClientVLESS = "127.0.0.1:55444"
)

func API(t testing.TB) string {
	t.Helper()
	addr := os.Getenv("SYLPH_TEST_XRAY")
	if addr == "" {
		t.Skip("SYLPH_TEST_XRAY not set, run make test-xray")
	}
	return addr
}

func NewUUID() string {
	return logging.NewRequestID()
}

func Exchange(vlessAddr string, uuid string, payload []byte, reply int) error {
	id, err := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	if err != nil || len(id) != 16 {
		return errors.New("bad uuid")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	echoed := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			echoed <- err
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, got); err != nil {
			echoed <- err
			return
		}
		if !bytes.Equal(got, payload) {
			echoed <- errors.New("echo server got a different payload")
			return
		}
		_, err = conn.Write(bytes.Repeat([]byte{'x'}, reply))
		echoed <- err
	}()
	conn, err := net.DialTimeout("tcp", vlessAddr, 5*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	port := ln.Addr().(*net.TCPAddr).Port
	req := append([]byte{0}, id...)
	req = append(req, 0, 1, byte(port>>8), byte(port), 1, 127, 0, 0, 1)
	req = append(req, payload...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	resp := make([]byte, 2+reply)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("read reply: %w", err)
	}
	select {
	case err := <-echoed:
		return err
	case <-time.After(5 * time.Second):
		return errors.New("echo server did not finish")
	}
}

type StatsQuery func(ctx context.Context, pattern string) (map[string]int64, error)

func WaitTraffic(t testing.TB, query StatsQuery, uuid string, up int64, down int64) {
	t.Helper()
	upName := "user>>>" + uuid + ">>>traffic>>>uplink"
	downName := "user>>>" + uuid + ">>>traffic>>>downlink"
	var last map[string]int64
	var lastErr error
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		last, lastErr = query(context.Background(), "user>>>"+uuid+">>>")
		if lastErr == nil && last[upName] == up && last[downName] == down {
			return
		}
	}
	t.Fatalf("counters for user = %v (err %v), want up %d down %d", last, lastErr, up, down)
}
