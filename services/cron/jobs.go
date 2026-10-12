package cron

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/apiclient"
)

type CoreClient struct {
	api *apiclient.Client
}

func NewCoreClient(api *apiclient.Client) *CoreClient {
	return &CoreClient{api: api}
}

func Jobs(core *CoreClient, cfg *Config) []Job {
	return []Job{
		{Name: "expire", Every: cfg.ExpireEvery, Do: core.expire},
		{Name: "cleanup", Every: cfg.CleanupEvery, Do: func(ctx context.Context) ([]slog.Attr, error) {
			return core.cleanup(ctx, cfg.RetentionDays)
		}},
	}
}

func (c *CoreClient) expire(ctx context.Context) ([]slog.Attr, error) {
	var out struct {
		Expired int64 `json:"expired"`
	}
	if err := c.api.Do(ctx, http.MethodPost, "/v1/maintenance/expire", nil, &out); err != nil {
		return nil, err
	}
	return []slog.Attr{slog.Int64("expired", out.Expired)}, nil
}

func (c *CoreClient) cleanup(ctx context.Context, retentionDays int) ([]slog.Attr, error) {
	var out struct {
		Deltas  int64 `json:"deltas"`
		Batches int64 `json:"batches"`
		Keys    int64 `json:"keys"`
	}
	if err := c.api.Do(ctx, http.MethodPost, "/v1/maintenance/cleanup", map[string]int{"older_than_days": retentionDays}, &out); err != nil {
		return nil, err
	}
	return []slog.Attr{slog.Int64("deltas", out.Deltas), slog.Int64("batches", out.Batches), slog.Int64("keys", out.Keys)}, nil
}
