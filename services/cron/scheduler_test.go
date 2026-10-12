package cron

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestJobsRunAtStartAndThenEveryInterval(t *testing.T) {
	var fast, slow atomic.Int32
	logs := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Run(ctx, []Job{
			{Name: "fast", Every: 20 * time.Millisecond, Do: func(context.Context) ([]slog.Attr, error) { fast.Add(1); return nil, nil }},
			{Name: "slow", Every: time.Hour, Do: func(context.Context) ([]slog.Attr, error) {
				slow.Add(1)
				return []slog.Attr{slog.Int64("expired", 3)}, nil
			}},
		}, logging.NewWithWriter("cron", "info", logs))
		close(done)
	}()
	waitFor(t, "fast job to repeat", func() bool { return fast.Load() >= 3 })
	select {
	case <-done:
		t.Fatal("Run returned before shutdown: the cron process would exit right away")
	default:
	}
	if slow.Load() != 1 {
		t.Fatalf("hourly job ran %d times, want exactly once at start", slow.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop on cancel")
	}
	out := logs.String()
	for _, want := range []string{`"msg":"cron_job_done"`, `"job":"slow"`, `"expired":3`, `"job":"fast"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs lack %s:\n%s", want, out)
		}
	}
}

func TestFailedJobIsLoggedAndRetriedNextTick(t *testing.T) {
	var calls atomic.Int32
	logs := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, []Job{{Name: "flaky", Every: 20 * time.Millisecond, Do: func(context.Context) ([]slog.Attr, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("core unavailable")
		}
		return nil, nil
	}}}, logging.NewWithWriter("cron", "info", logs))
	waitFor(t, "retry after failure", func() bool { return calls.Load() >= 2 && strings.Contains(logs.String(), `"msg":"cron_job_done"`) })
	out := logs.String()
	if !strings.Contains(out, `"msg":"cron_job_failed"`) || !strings.Contains(out, `"error":"core unavailable"`) {
		t.Fatalf("failure not logged:\n%s", out)
	}
}

func TestSlowRunDoesNotOverlapItself(t *testing.T) {
	var running, overlaps atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	go Run(ctx, []Job{{Name: "slow", Every: 5 * time.Millisecond, Do: func(context.Context) ([]slog.Attr, error) {
		if running.Add(1) > 1 {
			overlaps.Add(1)
		}
		time.Sleep(30 * time.Millisecond)
		running.Add(-1)
		calls.Add(1)
		return nil, nil
	}}}, logging.NewWithWriter("cron", "error", &syncBuffer{}))
	waitFor(t, "a few slow runs", func() bool { return calls.Load() >= 3 })
	if overlaps.Load() != 0 {
		t.Fatalf("job overlapped itself %d times", overlaps.Load())
	}
}

func TestJobContextIsCanceledOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	stopped := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		Run(ctx, []Job{{Name: "long", Every: time.Hour, Do: func(jobCtx context.Context) ([]slog.Attr, error) {
			close(started)
			<-jobCtx.Done()
			stopped <- jobCtx.Err()
			return nil, jobCtx.Err()
		}}}, logging.NewWithWriter("cron", "error", &syncBuffer{}))
		close(done)
	}()
	<-started
	cancel()
	select {
	case err := <-stopped:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("job ctx err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("running job was not canceled")
	}
	<-done
}
