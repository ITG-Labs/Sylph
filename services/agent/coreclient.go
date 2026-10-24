package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/ITG-Labs/sylph/pkg/apiclient"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

type SyncUser struct {
	UUID     string `json:"uuid"`
	PlanID   int64  `json:"plan_id,omitempty"`
	EgressIP string `json:"egress_ip,omitempty"`
}

type GeoSource struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	SHA256URL string `json:"sha256_url"`
}

type SyncGeo struct {
	Generation int64       `json:"generation"`
	Sources    []GeoSource `json:"sources"`
}

type Reality struct {
	Dest        string   `json:"dest"`
	ServerNames []string `json:"server_names"`
	ShortIDs    []string `json:"short_ids"`
	Fingerprint string   `json:"fingerprint"`
	PrivateKey  string   `json:"private_key"`
}

type Inbound struct {
	Tag       string   `json:"tag"`
	Port      int      `json:"port"`
	Protocol  string   `json:"protocol"`
	Transport string   `json:"transport"`
	Security  string   `json:"security"`
	Flow      string   `json:"flow"`
	Reality   *Reality `json:"reality,omitempty"`
}

type SyncResp struct {
	Version     int64              `json:"version"`
	Users       []SyncUser         `json:"users"`
	Inbounds    []Inbound          `json:"inbounds"`
	Routing     *noderoute.Profile `json:"routing,omitempty"`
	ServiceKeys []string           `json:"service_keys,omitempty"`
	Geo         *SyncGeo           `json:"geo,omitempty"`
}

type Delta struct {
	KeyUUID   string `json:"key_uuid"`
	BytesUp   int64  `json:"bytes_up"`
	BytesDown int64  `json:"bytes_down"`
}

type Heartbeat struct {
	AgentVersion   string               `json:"agent_version"`
	XrayVersion    string               `json:"xray_version"`
	AgentUptimeSec int64                `json:"agent_uptime_sec"`
	XrayUptimeSec  *int64               `json:"xray_uptime_sec"`
	ActiveUsers    *int                 `json:"active_users"`
	ConfigError    *string              `json:"config_error"`
	GeoStatus      map[string]GeoStatus `json:"geo_status,omitempty"`
	RoutingError   *string              `json:"routing_error,omitempty"`
	Addresses      []AddressStatus      `json:"addresses"`
}

type CoreClient struct {
	api *apiclient.Client
}

func NewCoreClient(cfg *Config, logger *slog.Logger) (*CoreClient, error) {
	certPEM, err := os.ReadFile(cfg.TLSCert)
	if err != nil {
		return nil, fmt.Errorf("read tls cert: %w", err)
	}
	keyPEM, err := os.ReadFile(cfg.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("read tls key: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.CACert)
	if err != nil {
		return nil, fmt.Errorf("read ca cert: %w", err)
	}
	tlsCfg, err := mtls.NewClientTLSConfig(mtls.ClientConfig{
		CertPEM:    certPEM,
		KeyPEM:     keyPEM,
		CAPEM:      caPEM,
		ServerName: cfg.ServerName,
	})
	if err != nil {
		return nil, fmt.Errorf("tls config: %w", err)
	}
	api, err := apiclient.New("agent", cfg.CoreURL, tlsCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("api client: %w", err)
	}
	return &CoreClient{api: api}, nil
}

func newCoreClientForTest(cfg *Config, logger *slog.Logger) *CoreClient {
	api, err := apiclient.New("agent", cfg.CoreURL, nil, logger)
	if err != nil {
		panic(err)
	}
	return &CoreClient{api: api}
}

func (c *CoreClient) SyncUsers(ctx context.Context, knownVersion int64) (*SyncResp, error) {
	var resp SyncResp
	if err := c.api.Do(ctx, http.MethodPost, "/v1/sync/users", map[string]int64{"known_version": knownVersion}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *CoreClient) ReportDeltas(ctx context.Context, batch *Batch) (bool, error) {
	var resp struct {
		Duplicate bool `json:"duplicate"`
	}
	if err := c.api.Do(ctx, http.MethodPost, "/v1/sync/deltas", batch, &resp); err != nil {
		return false, err
	}
	return resp.Duplicate, nil
}

func (c *CoreClient) Heartbeat(ctx context.Context, hb Heartbeat) error {
	return c.api.Do(ctx, http.MethodPost, "/v1/sync/heartbeat", hb, nil)
}

type GeoStatus struct {
	SHA256       string `json:"sha256,omitempty"`
	Size         int64  `json:"size,omitempty"`
	DownloadedAt string `json:"downloaded_at,omitempty"`
	Error        string `json:"error,omitempty"`
}
