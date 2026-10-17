package pulse

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"
)

type fakeDoer struct {
	mu     sync.Mutex
	bodies []Body
	paths  []string
	fail   bool
}

func (f *fakeDoer) Do(_ context.Context, method, path string, in, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if method != http.MethodPost {
		return errors.New("unexpected method " + method)
	}
	f.paths = append(f.paths, path)
	f.bodies = append(f.bodies, in.(Body))
	if f.fail {
		return errors.New("core down")
	}
	return nil
}

func (f *fakeDoer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func TestRunSendsPulsesUntilCancelled(t *testing.T) {
	d := &fakeDoer{fail: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	conflict := time.Now().UTC()
	go func() {
		Run(ctx, d, slog.New(slog.NewTextHandler(io.Discard, nil)), 20*time.Millisecond, func(b *Body) { b.TelegramConflictAt = &conflict })
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for d.count() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	if d.count() < 3 {
		t.Fatalf("pulses = %d, want at least 3 (failures must not stop the loop)", d.count())
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.paths[0] != "/v1/services/heartbeat" || d.bodies[0].Version == "" || d.bodies[0].TelegramConflictAt == nil {
		t.Fatalf("first pulse = %s %+v", d.paths[0], d.bodies[0])
	}
	if d.bodies[len(d.bodies)-1].UptimeSec < d.bodies[0].UptimeSec {
		t.Fatalf("uptime went back: %+v", d.bodies)
	}
}
