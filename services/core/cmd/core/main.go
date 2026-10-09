package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/logging"
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
	_ = time.Now()
	return nil
}

func envOr(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
