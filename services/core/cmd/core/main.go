package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/healthcheck"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/api"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
	"github.com/ITG-Labs/sylph/services/core/internal/yookassa"
	_ "time/tzdata"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck.Run(healthcheck.LocalURL(envOr("SYLPH_HEALTH_LISTEN", "127.0.0.1:8081"), "/health"), os.Stderr))
	}
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
	tz, err := statsTimezone()
	if err != nil {
		return err
	}
	if err := st.SetStatsTimezone(ctx, tz); err != nil {
		return fmt.Errorf("SYLPH_STATS_TZ: database does not know %q: %w", tz, err)
	}
	logger.Info("stats_timezone", slog.String("tz", tz))
	svc := service.NewService(st, logger)
	offline, stale, err := nodeThresholds()
	if err != nil {
		return err
	}
	svc.SetNodeThresholds(offline, stale)
	bot, err := botUsername()
	if err != nil {
		return err
	}
	svc.SetBotUsername(bot)
	payCfg, err := paymentsConfig()
	if err != nil {
		return err
	}
	if payCfg == nil {
		logger.Warn("yookassa_disabled")
	} else {
		svc.EnablePayments(yookassa.New(yookassa.Config{BaseURL: payCfg.API, ShopID: payCfg.ShopID, SecretKey: payCfg.SecretKey}), payCfg.ReturnURL)
		logger.Info("yookassa_ready", slog.String("api", payCfg.API), slog.String("shop_id", payCfg.ShopID))
	}

	tlsConfig, err := loadTLSConfig()
	if err != nil {
		return err
	}
	revoked := loadRevokedList(envOr("SYLPH_REVOKED_JSON", ""))
	serverNotAfter, err := certNotAfter(envOr("SYLPH_TLS_CERT", ""))
	if err != nil {
		return err
	}
	caNotAfter, err := certNotAfter(envOr("SYLPH_CLIENT_CA", ""))
	if err != nil {
		return err
	}
	svc.SetCertExpiry(serverNotAfter, caNotAfter)
	logger.Info("tls_ready", slog.Time("cert_not_after", serverNotAfter), slog.Time("ca_not_after", caNotAfter))

	if addr := os.Getenv("SYLPH_HEALTH_LISTEN"); addr != "" {
		healthSrv := &http.Server{Addr: addr, Handler: healthHandler(pool.Ping, logger), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("health_listen_failed", slog.String("error", err.Error()))
			}
		}()
		defer healthSrv.Close()
		logger.Info("health_listening", slog.String("addr", addr))
	}

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

type payments struct {
	ShopID    string
	SecretKey string
	ReturnURL string
	API       string
}

func paymentsConfig() (*payments, error) {
	cfg := &payments{
		ShopID:    os.Getenv("SYLPH_YOOKASSA_SHOP_ID"),
		SecretKey: os.Getenv("SYLPH_YOOKASSA_SECRET_KEY"),
		ReturnURL: os.Getenv("SYLPH_YOOKASSA_RETURN_URL"),
		API:       envOr("SYLPH_YOOKASSA_API", "https://api.yookassa.ru/v3"),
	}
	if cfg.ShopID == "" && cfg.SecretKey == "" && cfg.ReturnURL == "" {
		return nil, nil
	}
	if cfg.ShopID == "" || cfg.SecretKey == "" || cfg.ReturnURL == "" {
		return nil, errors.New("SYLPH_YOOKASSA_SHOP_ID, SYLPH_YOOKASSA_SECRET_KEY and SYLPH_YOOKASSA_RETURN_URL must be set together")
	}
	if !httpURL(cfg.ReturnURL) {
		return nil, errors.New("SYLPH_YOOKASSA_RETURN_URL must be an http(s) URL")
	}
	if !httpURL(cfg.API) {
		return nil, errors.New("SYLPH_YOOKASSA_API must be an http(s) URL")
	}
	return cfg, nil
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
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

func statsTimezone() (string, error) {
	tz := envOr("SYLPH_STATS_TZ", store.DefaultStatsTimezone)
	if _, err := time.LoadLocation(tz); err != nil {
		return "", fmt.Errorf("SYLPH_STATS_TZ: %w", err)
	}
	return tz, nil
}

var botUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`)

func botUsername() (string, error) {
	name := strings.TrimPrefix(os.Getenv("SYLPH_BOT_USERNAME"), "@")
	if name == "" {
		return "", nil
	}
	if !botUsernamePattern.MatchString(name) {
		return "", errors.New("SYLPH_BOT_USERNAME must be the bot username: 5-32 characters of A-Z, a-z, 0-9 and _")
	}
	return name, nil
}

func nodeThresholds() (time.Duration, time.Duration, error) {
	offline, err := positiveDuration("SYLPH_NODE_OFFLINE_AFTER", service.DefaultNodeOffline)
	if err != nil {
		return 0, 0, err
	}
	stale, err := positiveDuration("SYLPH_NODE_SYNC_STALE_AFTER", service.DefaultNodeSyncStale)
	if err != nil {
		return 0, 0, err
	}
	return offline, stale, nil
}

func positiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration like 90s or 3m", key)
	}
	return d, nil
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func healthHandler(ping func(context.Context) error, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		status, body := http.StatusOK, `{"status":"ok","version":"`+version.Version+`"}`
		if err := ping(ctx); err != nil {
			status, body = http.StatusServiceUnavailable, `{"status":"db_unavailable"}`
			logger.Warn("health_db_unavailable", slog.String("error", err.Error()))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := w.Write([]byte(body + "\n")); err != nil {
			logger.Warn("health_write_failed", slog.String("error", err.Error()))
		}
	})
	return mux
}

func certNotAfter(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("read cert %s: %w", path, err)
	}
	cert, err := mtls.ParseCertPEM(data)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cert %s: %w", path, err)
	}
	return cert.NotAfter, nil
}
