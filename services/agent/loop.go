package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
)

type Loop struct {
	engine *Engine
	stats  *StatsCollector
	core   *CoreClient
	cfg    *Config
	logger *slog.Logger
}

func NewLoop(e *Engine, sc *StatsCollector, core *CoreClient, cfg *Config, logger *slog.Logger) *Loop {
	return &Loop{engine: e, stats: sc, core: core, cfg: cfg, logger: logger}
}

func (l *Loop) RunOnce(ctx context.Context) error {
	_, syncErr := l.engine.SyncOnce(ctx)
	if syncErr != nil {
		l.logger.Error("sync_failed", "error", syncErr.Error())
	}
	l.reportTraffic(ctx)
	if err := l.core.Heartbeat(ctx, version.Version, ""); err != nil {
		l.logger.Error("heartbeat_failed", "error", err.Error())
	}
	return syncErr
}

func (l *Loop) reportTraffic(ctx context.Context) {
	deltas, err := l.stats.Collect(ctx, l.engine.ActiveUsers())
	if err != nil {
		l.logger.Error("stats_failed", "error", err.Error())
		return
	}
	if len(deltas) > 0 {
		if err := l.core.ReportDeltas(ctx, deltas); err != nil {
			l.logger.Error("deltas_report_failed", "error", err.Error(), "pending", len(deltas))
			return
		}
		l.logger.Info("deltas_reported", "count", len(deltas))
	}
	l.stats.Commit()
}

func (l *Loop) Run(ctx context.Context) error {
	ticker := time.NewTicker(l.cfg.SyncInterval)
	defer ticker.Stop()
	for {
		if err := l.RunOnce(ctx); err != nil {
			l.logger.Error("tick_failed", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
