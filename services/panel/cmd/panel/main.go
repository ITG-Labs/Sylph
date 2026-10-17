package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/apiclient"
	"github.com/ITG-Labs/sylph/pkg/healthcheck"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/pkg/pulse"
	"github.com/ITG-Labs/sylph/services/panel"
	"github.com/ITG-Labs/sylph/services/panel/ui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck.Run(healthcheck.LocalURL(envOr("SYLPH_LISTEN", ":8080"), "/healthz"), os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "new-admin" {
		if err := newAdmin(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	logger := logging.New("panel", envOr("SYLPH_LOG_LEVEL", "info"))
	logger.Info("starting", slog.String("version", version.Version))
	if err := run(logger); err != nil {
		logger.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func newAdmin(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("new-admin", flag.ContinueOnError)
	name := fs.String("name", "", "admin name shown in the audit log")
	asJSON := fs.Bool("json", false, "print the record, password and TOTP URI as one JSON object")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rec, password, uri, err := panel.NewAdmin(*name)
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(out).Encode(struct {
			Record   panel.AdminRecord `json:"record"`
			Password string            `json:"password"`
			TOTPURI  string            `json:"totp_uri"`
		}{rec, password, uri})
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Запись для файла админов (SYLPH_PANEL_ADMINS_FILE):\n%s\n\nПароль: %s\nTOTP: %s\n", b, password, uri)
	return nil
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := panel.LoadConfig()
	if err != nil {
		return err
	}
	admins, err := panel.LoadAdmins(cfg.AdminsFile)
	if err != nil {
		return err
	}
	client, tlsCfg, err := coreHTTPClient(cfg)
	if err != nil {
		return err
	}
	api, err := apiclient.New("panel", cfg.CoreURL, tlsCfg, logger)
	if err != nil {
		return err
	}
	go pulse.Run(ctx, api, logger, pulse.Every, nil)
	proxy, err := panel.NewCoreProxy(cfg.CoreURL, client, logger)
	if err != nil {
		return err
	}
	dist := ui.Dist()
	if dist == nil {
		logger.Warn("ui_not_built")
	}
	server := panel.NewServer(panel.NewAuth(admins, cfg.SessionTTL), proxy, cfg.TrustedProxies, dist, cfg.SessionTTL, logger)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", cfg.Listen), slog.Int("admins", len(admins)))
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

func coreHTTPClient(cfg *panel.Config) (*http.Client, *tls.Config, error) {
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
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg, ForceAttemptHTTP2: true}}, tlsCfg, nil
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
