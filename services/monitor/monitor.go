package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Source interface {
	State(ctx context.Context) (*State, error)
}

type Messenger interface {
	Send(ctx context.Context, chatID int64, text string) error
	Updates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error)
}

const commandMaxAge = 5 * time.Minute

type Monitor struct {
	src       Source
	msg       Messenger
	chats     []int64
	statePath string
	loc       *time.Location
	panelURL  string
	log       *slog.Logger
	now       func() time.Time

	mu      sync.Mutex
	tracker *Tracker
}

func NewMonitor(src Source, msg Messenger, chats []int64, statePath string, loc *time.Location, panelURL string, log *slog.Logger) *Monitor {
	m := &Monitor{src: src, msg: msg, chats: chats, statePath: statePath, loc: loc, panelURL: panelURL, log: log, now: time.Now, tracker: NewTracker()}
	data, err := os.ReadFile(statePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		log.Error("state_load_failed", slog.String("error", err.Error()))
	default:
		if err := m.tracker.UnmarshalJSON(data); err != nil {
			log.Error("state_load_failed", slog.String("error", err.Error()))
			m.tracker = NewTracker()
		}
	}
	return m
}

func (m *Monitor) Run(ctx context.Context, every time.Duration) {
	m.Tick(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Tick(ctx)
		}
	}
}

func (m *Monitor) Tick(ctx context.Context) {
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	st, err := m.src.State(callCtx)
	cancel()
	if ctx.Err() != nil {
		return
	}
	now := m.now()
	m.mu.Lock()
	var alerts []Alert
	if err != nil {
		m.log.Warn("core_unreachable", slog.String("error", err.Error()))
		alerts = m.tracker.CoreDown(now)
	} else {
		alerts = m.tracker.Step(now, st.Problems)
	}
	m.mu.Unlock()
	for _, a := range alerts {
		if !m.deliver(ctx, FormatAlert(a, now, m.loc, m.panelURL)) {
			m.mu.Lock()
			m.tracker.Failed(a)
			m.mu.Unlock()
			continue
		}
		m.log.Info("alert_sent", slog.String("kind", a.Kind), slog.String("key", a.Problem.Key), slog.Int("level", a.Problem.Level))
	}
	m.save()
}

func (m *Monitor) deliver(ctx context.Context, text string) bool {
	ok := false
	for _, chat := range m.chats {
		if err := m.msg.Send(ctx, chat, text); err != nil {
			m.log.Warn("telegram_send_failed", slog.Int64("chat", chat), slog.String("error", err.Error()))
			continue
		}
		ok = true
	}
	return ok
}

func (m *Monitor) save() {
	m.mu.Lock()
	data, err := m.tracker.MarshalJSON()
	m.mu.Unlock()
	if err == nil {
		err = writeAtomic(m.statePath, data)
	}
	if err != nil {
		m.log.Error("state_save_failed", slog.String("error", err.Error()))
	}
}

func (m *Monitor) Poll(ctx context.Context) {
	var offset int64
	for ctx.Err() == nil {
		ups, err := m.msg.Updates(ctx, offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.log.Warn("telegram_updates_failed", slog.String("error", err.Error()))
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range ups {
			offset = u.ID + 1
			m.handle(ctx, u)
		}
	}
}

func (m *Monitor) handle(ctx context.Context, u Update) {
	msg := u.Message
	if msg == nil || !slices.Contains(m.chats, msg.Chat.ID) {
		return
	}
	if m.now().Sub(time.Unix(msg.Date, 0)) > commandMaxAge {
		return
	}
	fields := strings.Fields(msg.Text)
	if len(fields) == 0 {
		return
	}
	cmd, _, _ := strings.Cut(fields[0], "@")
	var reply string
	switch cmd {
	case "/status":
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		st, err := m.src.State(callCtx)
		cancel()
		reply = FormatStatus(st, err, m.now(), m.loc)
	case "/start":
		reply = "Привет! Сюда приходят алерты Sylph: 🔴 — сломалось, 🟢 — починилось.\n/status — что сломано прямо сейчас."
	default:
		return
	}
	if err := m.msg.Send(ctx, msg.Chat.ID, reply); err != nil {
		m.log.Warn("telegram_send_failed", slog.Int64("chat", msg.Chat.ID), slog.String("error", err.Error()))
	}
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
