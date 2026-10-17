package monitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSource struct {
	mu    sync.Mutex
	state *State
	err   error
}

func (f *fakeSource) State(context.Context) (*State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, f.err
}

func (f *fakeSource) set(st *State, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state, f.err = st, err
}

type sent struct {
	chat int64
	text string
}

type fakeMessenger struct {
	mu      sync.Mutex
	sent    []sent
	fail    map[int64]bool
	updates []Update
}

func (f *fakeMessenger) Send(_ context.Context, chat int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[chat] {
		return errors.New("telegram down")
	}
	f.sent = append(f.sent, sent{chat, text})
	return nil
}

func (f *fakeMessenger) Updates(context.Context, int64, int) ([]Update, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ups := f.updates
	f.updates = nil
	return ups, nil
}

func (f *fakeMessenger) take() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.sent
	f.sent = nil
	return out
}

func newTestMonitor(t *testing.T, src Source, msg Messenger, path string, clock *time.Time) *Monitor {
	t.Helper()
	m := NewMonitor(src, msg, []int64{111, 222}, path, time.UTC, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.now = func() time.Time { return *clock }
	return m
}

func stateWith(problems ...Problem) *State {
	return &State{Problems: problems, Nodes: map[string]int{"online": 1}}
}

func TestTickAlertsEveryChatAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := t0
	src := &fakeSource{state: stateWith(Problem{Key: "money:mismatch", Level: 2, Text: "mismatch: 1"})}
	msg := &fakeMessenger{fail: map[int64]bool{}}
	m := newTestMonitor(t, src, msg, path, &now)
	m.Tick(context.Background())
	got := msg.take()
	if len(got) != 2 || got[0].chat != 111 || got[1].chat != 222 || !strings.Contains(got[0].text, "mismatch: 1") {
		t.Fatalf("sent = %+v", got)
	}
	restarted := newTestMonitor(t, src, msg, path, &now)
	now = now.Add(time.Minute)
	restarted.Tick(context.Background())
	if got := msg.take(); len(got) != 0 {
		t.Fatalf("restart re-sent: %+v", got)
	}
	src.set(stateWith(), nil)
	now = now.Add(time.Minute)
	restarted.Tick(context.Background())
	if got := msg.take(); len(got) != 2 || !strings.Contains(got[0].text, "Починилось") {
		t.Fatalf("resolve = %+v", got)
	}
}

func TestTickRetriesWhenNobodyGotTheAlert(t *testing.T) {
	now := t0
	src := &fakeSource{state: stateWith(Problem{Key: "money:mismatch", Level: 2, Text: "x"})}
	msg := &fakeMessenger{fail: map[int64]bool{111: true, 222: true}}
	m := newTestMonitor(t, src, msg, filepath.Join(t.TempDir(), "state.json"), &now)
	m.Tick(context.Background())
	msg.mu.Lock()
	msg.fail = map[int64]bool{222: true}
	msg.mu.Unlock()
	now = now.Add(time.Minute)
	m.Tick(context.Background())
	if got := msg.take(); len(got) != 1 || got[0].chat != 111 {
		t.Fatalf("retry = %+v", got)
	}
	now = now.Add(time.Minute)
	m.Tick(context.Background())
	if got := msg.take(); len(got) != 0 {
		t.Fatalf("delivered to one chat, but sent again: %+v", got)
	}
}

func TestTickCoreUnreachable(t *testing.T) {
	now := t0
	src := &fakeSource{err: errors.New("dial tcp: connection refused")}
	msg := &fakeMessenger{fail: map[int64]bool{}}
	m := newTestMonitor(t, src, msg, filepath.Join(t.TempDir(), "state.json"), &now)
	m.Tick(context.Background())
	now = now.Add(time.Minute)
	m.Tick(context.Background())
	if got := msg.take(); len(got) != 0 {
		t.Fatalf("alerted before the delay: %+v", got)
	}
	now = now.Add(time.Minute)
	m.Tick(context.Background())
	if got := msg.take(); len(got) != 2 || !strings.Contains(got[0].text, "Ядро недоступно") {
		t.Fatalf("core down = %+v", got)
	}
	src.set(stateWith(), nil)
	now = now.Add(time.Minute)
	m.Tick(context.Background())
	if got := msg.take(); len(got) != 2 || !strings.Contains(got[0].text, "Починилось") {
		t.Fatalf("core back = %+v", got)
	}
}

func TestCommandsAnswerOnlyKnownChats(t *testing.T) {
	now := time.Now()
	src := &fakeSource{state: stateWith(Problem{Key: "node:1:offline", Level: 2, Text: "Нода «A» лежит"})}
	msg := &fakeMessenger{fail: map[int64]bool{}}
	m := newTestMonitor(t, src, msg, filepath.Join(t.TempDir(), "state.json"), &now)
	mk := func(chat int64, text string, age time.Duration) Update {
		u := Update{Message: &Message{Text: text, Date: now.Add(-age).Unix()}}
		u.Message.Chat.ID = chat
		return u
	}
	m.handle(context.Background(), mk(999, "/status", 0))
	m.handle(context.Background(), mk(111, "/status", 10*time.Minute))
	m.handle(context.Background(), mk(111, "hello", 0))
	if got := msg.take(); len(got) != 0 {
		t.Fatalf("answered a stranger, an old message or chatter: %+v", got)
	}
	m.handle(context.Background(), mk(111, "/status@sylph_alerts_bot", 0))
	got := msg.take()
	if len(got) != 1 || got[0].chat != 111 || !strings.Contains(got[0].text, "Нода «A» лежит") {
		t.Fatalf("status = %+v", got)
	}
	m.handle(context.Background(), mk(222, "/start", 0))
	if got := msg.take(); len(got) != 1 || !strings.Contains(got[0].text, "/status") {
		t.Fatalf("start = %+v", got)
	}
}

func TestBrokenStateFileStartsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := writeAtomic(path, []byte("{broken")); err != nil {
		t.Fatal(err)
	}
	now := t0
	src := &fakeSource{state: stateWith(Problem{Key: "money:mismatch", Level: 2, Text: "x"})}
	msg := &fakeMessenger{fail: map[int64]bool{}}
	m := newTestMonitor(t, src, msg, path, &now)
	m.Tick(context.Background())
	if got := msg.take(); len(got) != 2 {
		t.Fatalf("sent = %+v", got)
	}
	broken, err := filepath.Glob(path + ".broken-*")
	if err != nil || len(broken) != 1 {
		t.Fatalf("broken state was not kept aside: %v %v", broken, err)
	}
}
