package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/services/agent"
)

func main() {
	logger := logging.New("agent", envOr("SYLPH_LOG_LEVEL", "info"))
	logger.Info("starting", slog.String("version", version.Version))
	if err := run(logger); err != nil {
		logger.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := agent.LoadConfig()
	if err != nil {
		return err
	}
	core, err := agent.NewCoreClient(cfg, logger)
	if err != nil {
		return fmt.Errorf("core client: %w", err)
	}
	xray, err := dialXray()
	if err != nil {
		return fmt.Errorf("xray: %w", err)
	}
	engine := agent.NewEngine(cfg, core, xray, logger)
	if err := engine.LoadState(cfg.CachePath); err != nil {
		logger.Error("cache_load_failed", slog.String("error", err.Error()))
	}
	stats := agent.NewStatsCollector(xray, logger)
	if err := stats.Load(cfg.StatsPath); err != nil {
		return fmt.Errorf("stats state: %w", err)
	}
	loop := agent.NewLoop(engine, stats, core, xray, cfg, logger)
	logger.Info("loop_started", slog.String("interval", cfg.SyncInterval.String()))
	return loop.Run(ctx)
}

func dialXray() (agent.XrayClient, error) {
	return nil, errors.New("xray grpc adapter is not implemented yet")
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
