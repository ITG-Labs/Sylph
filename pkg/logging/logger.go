package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func New(service string, level string) *slog.Logger {
	return NewWithWriter(service, level, os.Stdout)
}

func NewWithWriter(service string, level string, w io.Writer) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(h).With(slog.String("service", service))
}

func parseLevel(level string) slog.Level {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
