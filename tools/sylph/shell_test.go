package main

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func newTestShell() *shell {
	return &shell{input: textinput.New(), installed: []string{"node", "panel"}, width: 100, height: 40}
}

func TestRootShellHidesCertCommands(t *testing.T) {
	m := newTestShell()
	m.root = true
	m.input.SetValue("/")
	for _, s := range m.suggestions() {
		if s.value == "/bundle" || s.value == "/revoke" || s.value == "/certs" {
			t.Fatalf("root shell offers %s", s.value)
		}
	}
	m.input.SetValue("/bundle core")
	press(m, "enter")
	if m.busy || !strings.Contains(m.log[len(m.log)-1], "только на ноуте") {
		t.Fatalf("root /bundle = %q busy=%v", m.log[len(m.log)-1], m.busy)
	}
}

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		"install panel":                 {"install", "panel"},
		`install panel --admin "Иван П"`: {"install", "panel", "--admin", "Иван П"},
		"  bundle   node --name 'a b' ":  {"bundle", "node", "--name", "a b"},
		`x ""`:                          {"x", ""},
	}
	for in, want := range cases {
		got, err := splitArgs(in)
		if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("splitArgs(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := splitArgs(`install "panel`); err == nil {
		t.Fatal("open quote must fail")
	}
}

func TestSuggestions(t *testing.T) {
	m := newTestShell()
	m.input.SetValue("/st")
	got := m.suggestions()
	if len(got) != 3 || got[0].value != "/status" || got[1].value != "/start" || got[2].value != "/stop" {
		t.Fatalf("/st = %+v", got)
	}
	m.input.SetValue("/bu")
	if got := m.suggestions(); len(got) != 1 || !got[0].more {
		t.Fatalf("/bundle needs an argument: %+v", got)
	}
	m.input.SetValue("/stop ")
	if got := m.suggestions(); len(got) != 2 || got[0].value != "/stop node" {
		t.Fatalf("/stop offers installed roles: %+v", got)
	}
	m.input.SetValue("/install co")
	if got := m.suggestions(); len(got) != 1 || got[0].value != "/install core" || got[0].desc == "" {
		t.Fatalf("/install co = %+v", got)
	}
	m.input.SetValue("/install core --dir")
	if got := m.suggestions(); got != nil {
		t.Fatalf("no suggestions after flags: %+v", got)
	}
	m.input.SetValue("status")
	if got := m.suggestions(); got != nil {
		t.Fatalf("no palette without slash: %+v", got)
	}
	m.hidePal = true
	m.input.SetValue("/st")
	if got := m.suggestions(); got != nil {
		t.Fatal("esc hides the palette")
	}
}

func press(m *shell, key string) tea.Cmd {
	var msg tea.KeyPressMsg
	switch key {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "ctrl+c":
		msg = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		msg = tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
	}
	return m.key(msg)
}

func TestShellBuiltins(t *testing.T) {
	m := newTestShell()
	m.input.SetValue("/he")
	press(m, "enter")
	if !strings.Contains(strings.Join(m.log, "\n"), "/install [роль]") {
		t.Fatalf("help not printed: %q", m.log)
	}
	if len(m.history) != 1 || m.history[0] != "/help" {
		t.Fatalf("history = %q", m.history)
	}
	m.input.SetValue("/nope")
	press(m, "enter")
	if !strings.Contains(m.log[len(m.log)-1], "нет такой команды") {
		t.Fatalf("unknown command: %q", m.log[len(m.log)-1])
	}
	m.input.SetValue("/clear")
	press(m, "enter")
	if len(m.log) != 0 {
		t.Fatalf("clear left %q", m.log)
	}
	press(m, "up")
	if m.input.Value() != "/clear" {
		t.Fatalf("history up = %q", m.input.Value())
	}
}

func TestShellTabCompletesArgumentCommands(t *testing.T) {
	m := newTestShell()
	m.input.SetValue("/bun")
	press(m, "enter")
	if m.input.Value() != "/bundle " || m.busy {
		t.Fatalf("enter on /bundle should wait for a role: %q busy=%v", m.input.Value(), m.busy)
	}
	m.input.SetValue("/sta")
	press(m, "tab")
	if m.input.Value() != "/status" {
		t.Fatalf("tab = %q", m.input.Value())
	}
}

func TestShellCtrlC(t *testing.T) {
	m := newTestShell()
	m.input.SetValue("/sta")
	if cmd := press(m, "ctrl+c"); cmd != nil || m.input.Value() != "" {
		t.Fatal("first ctrl+c clears the input")
	}
	if cmd := press(m, "ctrl+c"); cmd == nil || !m.quitting {
		t.Fatal("ctrl+c on empty input quits")
	}
	cancelled := false
	m = newTestShell()
	m.busy, m.cancel = true, func() { cancelled = true }
	press(m, "ctrl+c")
	if !cancelled || m.quitting {
		t.Fatal("ctrl+c while busy cancels the command")
	}
}
