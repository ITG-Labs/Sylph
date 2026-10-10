package main

import (
	"log/slog"
	"os"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/services/agent"
)

func main() {
	logger := logging.New("agent", envOrLevel())
	logger.Info("starting", slog.String("version", version.Version))
	if err := run(logger); err != nil {
		logger.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := agent.LoadConfig()
	if err != nil {
		return err
	}
	logger.Info("config_loaded", slog.String("core", cfg.CoreURL), slog.String("interval", cfg.SyncInterval.String()))
	return nil
}

func envOrLevel() string {
	if v := os.Getenv("SYLPH_LOG_LEVEL"); v != "" {
		return v
	}
	return "info"
}
