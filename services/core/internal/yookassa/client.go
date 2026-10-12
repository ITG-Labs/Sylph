package yookassa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("yookassa: payment not found")

type Config struct {
	BaseURL    string
	ShopID     string
	SecretKey  string
	Timeout    time.Duration
	RetryDelay time.Duration
}

type Client struct {
	base       string
	shopID     string
	secret     string
	http       *http.Client
	retryDelay time.Duration
}

func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	delay := cfg.RetryDelay
	if delay == 0 {
		delay = time.Second
	}
	return &Client{
		base:       strings.TrimRight(cfg.BaseURL, "/"),
		shopID:     cfg.ShopID,
		secret:     cfg.SecretKey,
		http:       &http.Client{Timeout: timeout},
		retryDelay: delay,
	}
}

type Amount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

func RUB(rub int64) Amount {
	return Amount{Value: strconv.FormatInt(rub, 10) + ".00", Currency: "RUB"}
}

func (a Amount) Kopecks() (int64, bool) {
	whole, frac, ok := strings.Cut(a.Value, ".")
	if !ok || len(frac) != 2 || whole == "" || !digits(whole) || !digits(frac) {
		return 0, false
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > (1<<62)/100 {
		return 0, false
	}
	f, _ := strconv.ParseInt(frac, 10, 64)
	return w*100 + f, true
}

func digits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type Confirmation struct {
	Type            string `json:"type"`
	ReturnURL       string `json:"return_url,omitempty"`
	ConfirmationURL string `json:"confirmation_url,omitempty"`
}

type CreateRequest struct {
	Amount       Amount            `json:"amount"`
	Capture      bool              `json:"capture"`
	Confirmation Confirmation      `json:"confirmation"`
	Description  string            `json:"description,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type CancellationDetails struct {
	Party  string `json:"party"`
	Reason string `json:"reason"`
}

type Payment struct {
	ID                  string               `json:"id"`
	Status              string               `json:"status"`
	Paid                bool                 `json:"paid"`
	Amount              Amount               `json:"amount"`
	Confirmation        *Confirmation        `json:"confirmation"`
	Metadata            map[string]string    `json:"metadata"`
	Test                bool                 `json:"test"`
	CancellationDetails *CancellationDetails `json:"cancellation_details"`
}

type APIError struct {
	Status      int
	Code        string
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("yookassa: HTTP %d %s: %s", e.Status, e.Code, e.Description)
}

func (c *Client) CreatePayment(ctx context.Context, req CreateRequest, idempotenceKey string) (*Payment, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, http.MethodPost, "/payments", body, idempotenceKey)
}

func (c *Client) GetPayment(ctx context.Context, id string) (*Payment, error) {
	return c.do(ctx, http.MethodGet, "/payments/"+url.PathEscape(id), nil, "")
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, idempotenceKey string) (*Payment, error) {
	p, err := c.once(ctx, method, path, body, idempotenceKey)
	if err == nil || !retryable(err) {
		return p, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(c.retryDelay):
	}
	return c.once(ctx, method, path, body, idempotenceKey)
}

func retryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status >= 500
	}
	return !errors.Is(err, ErrNotFound) && !errors.Is(err, context.Canceled)
}

func (c *Client) once(ctx context.Context, method, path string, body []byte, idempotenceKey string) (*Payment, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("yookassa: build request: %w", err)
	}
	req.SetBasicAuth(c.shopID, c.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotenceKey != "" {
		req.Header.Set("Idempotence-Key", idempotenceKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yookassa: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("yookassa: read response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := &APIError{Status: resp.StatusCode}
		var payload struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		}
		if json.Unmarshal(data, &payload) == nil {
			apiErr.Code, apiErr.Description = payload.Code, payload.Description
		}
		return nil, apiErr
	}
	var p Payment
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("yookassa: decode payment: %w", err)
	}
	return &p, nil
}
