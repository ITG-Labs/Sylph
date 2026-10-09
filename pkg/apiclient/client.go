package apiclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

type Client struct {
	base   *url.URL
	http   *http.Client
	logger *slog.Logger
}

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func New(service string, baseURL string, tlsConfig *tls.Config, logger *slog.Logger) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	if logger == nil {
		logger = logging.New(service, "info")
	}
	return &Client{
		base:   u,
		http:   &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}},
		logger: logger,
	}, nil
}

func (c *Client) Do(ctx context.Context, method string, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.JoinPath(path).String(), body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	reqID := logging.RequestIDFromContext(ctx)
	if reqID == "" {
		reqID = logging.NewRequestID()
	}
	req.Header.Set(logging.HeaderRequestID, reqID)
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		c.logger.LogAttrs(ctx, slog.LevelError, "api_request_failed",
			slog.String("method", method),
			slog.String("path", path),
			slog.String("request_id", reqID),
			slog.Duration("duration", time.Since(start)),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	c.logger.LogAttrs(ctx, slog.LevelInfo, "api_request",
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("status", resp.StatusCode),
		slog.String("request_id", reqID),
		slog.Duration("duration", time.Since(start)),
	)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode, RequestID: reqID}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr == nil && len(b) > 0 {
			var payload struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(b, &payload) == nil {
				apiErr.Code = payload.Code
				apiErr.Message = payload.Message
			} else {
				apiErr.Message = string(b)
			}
		}
		return apiErr
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response %s %s: %w", method, path, err)
		}
	}
	return nil
}

func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var hr HealthResponse
	if err := c.Do(ctx, http.MethodGet, "/health", nil, &hr); err != nil {
		return nil, err
	}
	return &hr, nil
}
