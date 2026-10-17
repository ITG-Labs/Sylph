package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/apiclient"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/pkg/pulse"
	"github.com/ITG-Labs/sylph/services/cron"
)

func main() {
	logger := logging.New("cron", envOr("SYLPH_LOG_LEVEL", "info"))
	logger.Info("starting", slog.String("version", version.Version))
	if err := run(logger); err != nil {
		logger.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := cron.LoadConfig()
	if err != nil {
		return err
	}
	core, api, err := coreClient(cfg, logger)
	if err != nil {
		return err
	}
	go pulse.Run(ctx, api, logger, pulse.Every, nil)
	logger.Info("scheduled",
		slog.String("expire_every", cfg.ExpireEvery.String()),
		slog.String("cleanup_every", cfg.CleanupEvery.String()),
		slog.String("payments_every", cfg.PaymentsEvery.String()),
		slog.Int("retention_days", cfg.RetentionDays))
	cron.Run(ctx, cron.Jobs(core, cfg), logger)
	logger.Info("stopped")
	return nil
}

func coreClient(cfg *cron.Config, logger *slog.Logger) (*cron.CoreClient, *apiclient.Client, error) {
	certPEM, err := os.ReadFile(cfg.TLSCert)
	if err != nil {
		return nil, nil, fmt.Errorf("read tls cert: %w", err)
	}
	keyPEM, err := os.ReadFile(cfg.TLSKey)
	if err != nil {
		return nil, nil, fmt.Errorf("read tls key: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.CACert)
	if err != nil {
		return nil, nil, fmt.Errorf("read ca cert: %w", err)
	}
	tlsCfg, err := mtls.NewClientTLSConfig(mtls.ClientConfig{CertPEM: certPEM, KeyPEM: keyPEM, CAPEM: caPEM, ServerName: cfg.ServerName})
	if err != nil {
		return nil, nil, fmt.Errorf("tls config: %w", err)
	}
	api, err := apiclient.New("cron", cfg.CoreURL, tlsCfg, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("api client: %w", err)
	}
	return cron.NewCoreClient(api), api, nil
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
