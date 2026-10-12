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
	"github.com/ITG-Labs/sylph/services/bot"
)

func main() {
	logger := logging.New("bot", envOr("SYLPH_LOG_LEVEL", "info"))
	logger.Info("starting", slog.String("version", version.Version))
	if err := run(logger); err != nil {
		logger.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := bot.LoadConfig()
	if err != nil {
		return err
	}
	core, err := coreClient(cfg, logger)
	if err != nil {
		return err
	}
	app := bot.NewApp(core, cfg.SubsURL, cfg.Location, logger)
	tg, err := bot.NewTelegram(cfg.Token, cfg.TelegramAPI, app, logger)
	if err != nil {
		return err
	}
	var srv *http.Server
	errCh := make(chan error, 1)
	if cfg.WebhookListen != "" {
		hook := bot.NewWebhook(core, func(ctx context.Context, telegramID int64, paidUntil *time.Time) error {
			return app.NotifyPaid(ctx, tg, telegramID, paidUntil)
		}, cfg.YooKassaIPs, cfg.TrustedProxies, logger)
		mux := http.NewServeMux()
		mux.Handle("/yookassa", hook)
		srv = &http.Server{Addr: cfg.WebhookListen, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 60 * time.Second}
		go func() {
			logger.Info("webhook_listening", slog.String("addr", cfg.WebhookListen))
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("webhook: %w", err)
				stop()
			}
		}()
	} else {
		logger.Warn("webhook_disabled")
	}
	logger.Info("polling", slog.String("subs_url", cfg.SubsURL))
	tg.Start(ctx)
	if srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	select {
	case err := <-errCh:
		return err
	default:
	}
	logger.Info("stopped")
	return nil
}

func coreClient(cfg *bot.Config, logger *slog.Logger) (*bot.CoreClient, error) {
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
	api, err := apiclient.New("bot", cfg.CoreURL, tlsCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("api client: %w", err)
	}
	return bot.NewCoreClient(api), nil
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
