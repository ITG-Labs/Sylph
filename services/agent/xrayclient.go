package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

type XrayClient interface {
	AddUser(ctx context.Context, inboundTag string, uuid string, flow string) error
	RemoveUser(ctx context.Context, inboundTag string, uuid string) error
	ListUsers(ctx context.Context, inboundTag string) ([]string, error)
	QueryStats(ctx context.Context, pattern string) (map[string]int64, error)
	Uptime(ctx context.Context) (time.Duration, error)
	AppendRules(ctx context.Context, rules []noderoute.TailRule) error
	RemoveRule(ctx context.Context, tag string) error
	RuleTags(ctx context.Context) ([]string, error)
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
