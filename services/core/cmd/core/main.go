package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/api"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func main() {
	logLevel := envOr("SYLPH_LOG_LEVEL", "info")
	logger := logging.New("core", logLevel)
	logger.Info("starting", slog.String("version", version.Version))
	if err := run(logger); err != nil {
		logger.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := os.Getenv("SYLPH_DB_URL")
	if dbURL == "" {
		return errors.New("SYLPH_DB_URL is required")
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	if err := store.ApplyMigrations(ctx, pool); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	logger.Info("db_ready")

	st := store.NewStore(pool)
	svc := service.NewService(st, logger)

	tlsConfig, err := loadTLSConfig()
	if err != nil {
		return err
	}
	revoked := loadRevokedList(envOr("SYLPH_REVOKED_JSON", ""))
	logger.Info("tls_ready")

	server := api.NewServer(svc, logger, revoked)
	addr := envOr("SYLPH_LISTEN", ":8443")
	httpSrv := &http.Server{Addr: addr, Handler: server.Handler(), TLSConfig: tlsConfig}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", addr))
		errCh <- httpSrv.ListenAndServeTLS("", "")
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
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("stopped")
	return nil
}

func loadTLSConfig() (*tls.Config, error) {
	certPEM, err := os.ReadFile(envOr("SYLPH_TLS_CERT", ""))
	if err != nil {
		return nil, fmt.Errorf("read server cert: %w", err)
	}
	keyPEM, err := os.ReadFile(envOr("SYLPH_TLS_KEY", ""))
	if err != nil {
		return nil, fmt.Errorf("read server key: %w", err)
	}
	caPEM, err := os.ReadFile(envOr("SYLPH_CLIENT_CA", ""))
	if err != nil {
		return nil, fmt.Errorf("read client ca: %w", err)
	}
	return mtls.NewServerTLSConfig(mtls.ServerConfig{
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
		ClientCAPEM: caPEM,
	})
}

func loadRevokedList(path string) *mtls.RevocationList {
	if path == "" {
		return mtls.NewRevocationList(nil)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mtls.NewRevocationList(nil)
		}
		panic(fmt.Sprintf("read revoked list: %v", err))
	}
	var list struct {
		Serials []string `json:"serials"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		panic(fmt.Sprintf("parse revoked list: %v", err))
	}
	return mtls.NewRevocationList(list.Serials)
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
