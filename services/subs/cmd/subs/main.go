package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/apiclient"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/subs"
)

func main() {
	logger := logging.New("subs", envOr("SYLPH_LOG_LEVEL", "info"))
	logger.Info("starting", slog.String("version", version.Version))
	if err := run(logger); err != nil {
		logger.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := subs.LoadConfig()
	for _, name := range subs.DeprecatedEnv() {
		logger.Warn("deprecated_env", slog.String("name", name), slog.String("hint", "ignored: set the subscription title and support link in the panel settings"))
	}
	if err != nil {
		return err
	}
	core, err := coreClient(cfg, logger)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           subs.NewServer(core, cfg.Settings, logger).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", cfg.Listen))
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen: %w", err)
		}
	case <-ctx.Done():
		logger.Info("shutdown_signal")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("stopped")
	return nil
}

func coreClient(cfg *subs.Config, logger *slog.Logger) (*subs.CoreClient, error) {
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
	tlsCfg, err := mtls.NewClientTLSConfig(mtls.ClientConfig{CertPEM: certPEM, KeyPEM: keyPEM, CAPEM: caPEM, ServerName: cfg.ServerName})
	if err != nil {
		return nil, fmt.Errorf("tls config: %w", err)
	}
	api, err := apiclient.New("subs", cfg.CoreURL, tlsCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("api client: %w", err)
	}
	return subs.NewCoreClient(api), nil
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
