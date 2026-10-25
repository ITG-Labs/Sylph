package cron

import (
	"context"
	"errors"
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
			return core.cleanup(ctx, cfg.RetentionDays, cfg.StatsRetentionDays, cfg.IPRetentionDays)
		}},
		{Name: "payments", Every: cfg.PaymentsEvery, Do: core.payments},
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

func (c *CoreClient) cleanup(ctx context.Context, retentionDays, statsRetentionDays, ipRetentionDays int) ([]slog.Attr, error) {
	var out struct {
		Deltas        int64 `json:"deltas"`
		Batches       int64 `json:"batches"`
		Keys          int64 `json:"keys"`
		Daily         int64 `json:"daily"`
		Notifications int64 `json:"notifications"`
		IPs           int64 `json:"ips"`
	}
	body := map[string]int{"older_than_days": retentionDays, "stats_older_than_days": statsRetentionDays, "ip_older_than_days": ipRetentionDays}
	if err := c.api.Do(ctx, http.MethodPost, "/v1/maintenance/cleanup", body, &out); err != nil {
		return nil, err
	}
	return []slog.Attr{slog.Int64("deltas", out.Deltas), slog.Int64("batches", out.Batches), slog.Int64("keys", out.Keys), slog.Int64("daily", out.Daily),
		slog.Int64("notifications", out.Notifications), slog.Int64("ips", out.IPs)}, nil
}

func (c *CoreClient) payments(ctx context.Context) ([]slog.Attr, error) {
	var out struct {
		Checked   int64 `json:"checked"`
		Succeeded int64 `json:"succeeded"`
		Canceled  int64 `json:"canceled"`
		Mismatch  int64 `json:"mismatch"`
		Failed    int64 `json:"failed"`
	}
	if err := c.api.Do(ctx, http.MethodPost, "/v1/maintenance/payments/poll", nil, &out); err != nil {
		var apiErr *apiclient.APIError
		if errors.As(err, &apiErr) && apiErr.Code == "payments_disabled" {
			return []slog.Attr{slog.Bool("disabled", true)}, nil
		}
		return nil, err
	}
	return []slog.Attr{
		slog.Int64("checked", out.Checked), slog.Int64("succeeded", out.Succeeded), slog.Int64("canceled", out.Canceled),
		slog.Int64("mismatch", out.Mismatch), slog.Int64("failed", out.Failed),
	}, nil
}
