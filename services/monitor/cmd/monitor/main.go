package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/apiclient"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/pkg/pulse"
	"github.com/ITG-Labs/sylph/services/monitor"
)

func main() {
	logger := logging.New("monitor", envOr("SYLPH_LOG_LEVEL", "info"))
	logger.Info("starting", slog.String("version", version.Version))
	if err := run(logger); err != nil {
		logger.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := monitor.LoadConfig()
	if err != nil {
		return err
	}
	certPEM, err := os.ReadFile(cfg.TLSCert)
	if err != nil {
		return fmt.Errorf("read tls cert: %w", err)
	}
	keyPEM, err := os.ReadFile(cfg.TLSKey)
	if err != nil {
		return fmt.Errorf("read tls key: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.CACert)
	if err != nil {
		return fmt.Errorf("read ca cert: %w", err)
	}
	tlsCfg, err := mtls.NewClientTLSConfig(mtls.ClientConfig{CertPEM: certPEM, KeyPEM: keyPEM, CAPEM: caPEM, ServerName: cfg.ServerName})
	if err != nil {
		return fmt.Errorf("tls config: %w", err)
	}
	api, err := apiclient.New("monitor", cfg.CoreURL, tlsCfg, logger)
	if err != nil {
		return fmt.Errorf("api client: %w", err)
	}
	m := monitor.NewMonitor(monitor.NewCoreClient(api), monitor.NewTelegram(cfg.TelegramAPI, cfg.BotToken),
		cfg.ChatIDs, cfg.StatePath, cfg.Location, cfg.PanelURL, logger)
	go pulse.Run(ctx, api, logger, pulse.Every, nil)
	go m.Poll(ctx)
	logger.Info("watching", slog.String("interval", cfg.Interval.String()), slog.Int("chats", len(cfg.ChatIDs)), slog.String("core", cfg.CoreURL))
	m.Run(ctx, cfg.Interval)
	logger.Info("stopped")
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
