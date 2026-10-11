package agent

import (
	"context"
	"log/slog"
	"time"
)

type XrayClient interface {
	AddUser(ctx context.Context, inboundTag string, uuid string) error
	RemoveUser(ctx context.Context, inboundTag string, uuid string) error
	ListUsers(ctx context.Context, inboundTag string) ([]string, error)
	QueryStats(ctx context.Context, pattern string) (map[string]int64, error)
	Uptime(ctx context.Context) (time.Duration, error)
}

func WaitForXray(ctx context.Context, x XrayClient, timeout time.Duration, step time.Duration, logger *slog.Logger) bool {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		uptime, err := x.Uptime(waitCtx)
		if err == nil {
			logger.Info("xray_ready", "xray_uptime_sec", int64(uptime/time.Second))
			return true
		}
		select {
		case <-waitCtx.Done():
			if ctx.Err() == nil {
				logger.Error("xray_not_ready", "error", err.Error(), "waited", timeout.String())
			}
			return false
		case <-time.After(step):
		}
	}
}
