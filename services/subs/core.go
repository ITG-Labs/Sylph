package subs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ITG-Labs/sylph/pkg/apiclient"
)

const (
	StatusOK              = "ok"
	StatusExpired         = "expired"
	StatusTrafficExceeded = "traffic_exceeded"
	StatusHWIDRequired    = "hwid_required"
	StatusDeviceLimit     = "device_limit"
	StatusTrialUsed       = "trial_used"
)

type Connection struct {
	Name        string          `json:"name"`
	Address     string          `json:"address"`
	Port        int             `json:"port"`
	UUID        string          `json:"uuid"`
	Flow        string          `json:"flow"`
	Transport   string          `json:"transport"`
	Security    string          `json:"security"`
	SNI         string          `json:"sni"`
	PublicKey   string          `json:"public_key"`
	ShortID     string          `json:"short_id"`
	Fingerprint string          `json:"fingerprint"`
	Path        string          `json:"path,omitempty"`
	Host        string          `json:"host,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	Extra       json.RawMessage `json:"extra,omitempty"`
	ALPN        []string        `json:"alpn,omitempty"`
}

type View struct {
	Status            string       `json:"status"`
	TrafficUsedBytes  int64        `json:"traffic_used_bytes"`
	TrafficLimitBytes *int64       `json:"traffic_limit_bytes"`
	ExpiresAt         *time.Time   `json:"expires_at"`
	Connections       []Connection `json:"connections"`
	Title             string       `json:"title"`
	SupportURL        string       `json:"support_url"`
}

var ErrNotFound = errors.New("subscription not found")

type Request struct {
	SubToken    string `json:"sub_token"`
	HWID        string `json:"hwid"`
	UserAgent   string `json:"user_agent"`
	DeviceOS    string `json:"device_os"`
	DeviceModel string `json:"device_model"`
	XHTTP       bool   `json:"xhttp"`
}

type Core interface {
	Subscription(ctx context.Context, req Request) (*View, error)
}

type CoreClient struct {
	api *apiclient.Client
}

func NewCoreClient(api *apiclient.Client) *CoreClient {
	return &CoreClient{api: api}
}

func (c *CoreClient) Subscription(ctx context.Context, req Request) (*View, error) {
	req.XHTTP = true
	var view View
	err := c.api.Do(ctx, http.MethodPost, "/v1/subscription", req, &view)
	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &view, nil
}
