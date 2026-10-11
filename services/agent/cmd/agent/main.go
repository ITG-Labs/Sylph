package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/services/agent"
	"github.com/ITG-Labs/sylph/services/agent/internal/xraygrpc"
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
	xray, err := xraygrpc.Dial(cfg.XrayAPI, cfg.VLESSFlow, cfg.XrayTimeout)
	if err != nil {
		return fmt.Errorf("xray: %w", err)
	}
	defer func() { _ = xray.Close() }()
	logger.Info("xray_api", slog.String("addr", cfg.XrayAPI), slog.String("flow", cfg.VLESSFlow))
	agent.WaitForXray(ctx, xray, 30*time.Second, time.Second, logger)
	if ctx.Err() != nil {
		return nil
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

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
