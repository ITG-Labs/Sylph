package monitor

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func moscow(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestFormatAlert(t *testing.T) {
	loc := moscow(t)
	now := time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)
	since := time.Date(2026, 10, 17, 9, 3, 0, 0, time.UTC)
	p := Problem{Key: "node:5:offline", Category: "node", Level: 2, Text: "Нода «DE-1» лежит: нет heartbeat", Since: &since}
	got := FormatAlert(Alert{Kind: KindProblem, Problem: p}, now, loc, "https://panel.example.com")
	for _, want := range []string{"🔴", "Нода «DE-1» лежит", "с 12:03 MSK", "https://panel.example.com/nodes/5"} {
		if !strings.Contains(got, want) {
			t.Errorf("alert %q lacks %q", got, want)
		}
	}
	warn := FormatAlert(Alert{Kind: KindProblem, Problem: Problem{Key: "money:pending", Category: "money", Level: 1, Text: "pending: 2"}}, now, loc, "")
	if !strings.HasPrefix(warn, "🟠") || strings.Contains(warn, "http") {
		t.Errorf("warning = %q", warn)
	}
	fixed := FormatAlert(Alert{Kind: KindResolved, Problem: p}, now, loc, "")
	if !strings.HasPrefix(fixed, "🟢") || !strings.Contains(fixed, "Починилось") || !strings.Contains(fixed, "DE-1") {
		t.Errorf("resolved = %q", fixed)
	}
	old := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	p.Since = &old
	if got := FormatAlert(Alert{Kind: KindProblem, Problem: p}, now, loc, ""); !strings.Contains(got, "с 16.10 01:00 MSK") {
		t.Errorf("old since = %q", got)
	}
}

func TestPanelLinks(t *testing.T) {
	cases := map[string]string{
		"node:5:offline":     "/nodes/5",
		"cert:node:node-de1": "/nodes",
		"money:mismatch":     "/payments",
		"service:bot:silent": "/",
		"cert:core":          "/",
		CoreUnreachableKey:   "",
	}
	for key, want := range cases {
		if got := panelPath(key); got != want {
			t.Errorf("panelPath(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestFormatStatus(t *testing.T) {
	loc := moscow(t)
	now := time.Now()
	st := &State{
		Problems: []Problem{{Key: "node:1:offline", Level: 2, Text: "Нода «A» лежит"}},
		Nodes:    map[string]int{"online": 2, "offline": 1, "degraded": 0, "never": 0, "disabled": 1},
		Services: []Service{{CertCN: "bot", Role: "bot", Version: "1.2.3", Alive: true}, {CertCN: "subs", Role: "subs", Version: "1.2.3", Alive: false}},
	}
	st.Core.Version = "1.2.3"
	got := FormatStatus(st, nil, now, loc)
	for _, want := range []string{"Проблем: 1", "Нода «A» лежит", "online 2", "offline 1", "Бот", "subs", "Ядро 1.2.3"} {
		if !strings.Contains(got, want) {
			t.Errorf("status lacks %q:\n%s", want, got)
		}
	}
	green := &State{Nodes: map[string]int{"online": 3}}
	if got := FormatStatus(green, nil, now, loc); !strings.Contains(got, "Всё зелёное") {
		t.Errorf("green = %s", got)
	}
	if got := FormatStatus(nil, errors.New("dial tcp: timeout"), now, loc); !strings.Contains(got, "Ядро недоступно") {
		t.Errorf("core down = %s", got)
	}
}
