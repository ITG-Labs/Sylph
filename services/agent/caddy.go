package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"time"
)

type CaddyAPI interface {
	Config(ctx context.Context) ([]byte, error)
	Load(ctx context.Context, cfg []byte) error
}

type CaddyRejectedError struct {
	Status  int
	Message string
}

func (e *CaddyRejectedError) Error() string {
	return fmt.Sprintf("caddy refused the config (%d): %s", e.Status, e.Message)
}

type caddyHTTP struct {
	base   string
	client *http.Client
}

func NewCaddyAPI(addr string, timeout time.Duration) CaddyAPI {
	network, address := "tcp", addr
	base := "http://" + addr
	if rest, ok := strings.CutPrefix(addr, "unix/"); ok {
		network, address = "unix", strings.SplitN(rest, "|", 2)[0]
		base = "http://localhost"
	}
	var d net.Dialer
	return &caddyHTTP{
		base: base,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return d.DialContext(ctx, network, address)
			}},
		},
	}
}

func (c *caddyHTTP) Config(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/config/", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caddy get config: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("caddy get config: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("caddy get config: status %d", resp.StatusCode)
	}
	return body, nil
}

func (c *caddyHTTP) Load(ctx context.Context, cfg []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/load", bytes.NewReader(cfg))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("caddy load: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var parsed struct {
		Error string `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &parsed) == nil && parsed.Error != "" {
		msg = parsed.Error
	}
	return &CaddyRejectedError{Status: resp.StatusCode, Message: msg}
}

func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
