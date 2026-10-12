package cron

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type Job struct {
	Name  string
	Every time.Duration
	Do    func(ctx context.Context) ([]slog.Attr, error)
}

func Run(ctx context.Context, jobs []Job, log *slog.Logger) {
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Go(func() { loop(ctx, job, log) })
	}
	wg.Wait()
}

func loop(ctx context.Context, job Job, log *slog.Logger) {
	ticker := time.NewTicker(job.Every)
	defer ticker.Stop()
	for {
		runOnce(ctx, job, log)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runOnce(ctx context.Context, job Job, log *slog.Logger) {
	start := time.Now()
	attrs, err := job.Do(ctx)
	attrs = append([]slog.Attr{slog.String("job", job.Name), slog.Duration("duration", time.Since(start))}, attrs...)
	if err != nil {
		log.LogAttrs(ctx, slog.LevelError, "cron_job_failed", append(attrs, slog.String("error", err.Error()))...)
		return
	}
	log.LogAttrs(ctx, slog.LevelInfo, "cron_job_done", attrs...)
}
