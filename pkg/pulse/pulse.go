package pulse

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
)

const Every = time.Minute

type Body struct {
	Version            string     `json:"version"`
	UptimeSec          int64      `json:"uptime_sec"`
	TelegramConflictAt *time.Time `json:"telegram_conflict_at,omitempty"`
}

type Doer interface {
	Do(ctx context.Context, method, path string, in, out any) error
}

var started = time.Now()

func Send(ctx context.Context, api Doer, log *slog.Logger, extra func(*Body)) {
	b := Body{Version: version.Version, UptimeSec: int64(time.Since(started).Seconds())}
	if extra != nil {
		extra(&b)
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := api.Do(callCtx, http.MethodPost, "/v1/services/heartbeat", b, nil); err != nil && ctx.Err() == nil {
		log.Debug("pulse_failed", slog.String("error", err.Error()))
	}
}

func Run(ctx context.Context, api Doer, log *slog.Logger, every time.Duration, extra func(*Body)) {
	Send(ctx, api, log, extra)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			Send(ctx, api, log, extra)
		}
	}
}
