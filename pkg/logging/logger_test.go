package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewWithWriterJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter("core", "info", &buf)
	logger.Info("hello", "key", "value")
	line := buf.String()
	if !strings.HasSuffix(line, "\n") {
		t.Fatal("log line should end with newline")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if m["service"] != "core" {
		t.Fatalf("service = %v, want core", m["service"])
	}
	if m["msg"] != "hello" {
		t.Fatalf("msg = %v, want hello", m["msg"])
	}
	if m["level"] != "INFO" {
		t.Fatalf("level = %v, want INFO", m["level"])
	}
	if m["key"] != "value" {
		t.Fatalf("key = %v, want value", m["key"])
	}
}

func TestNewWithWriterLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter("bot", "warn", &buf)
	logger.Info("nope")
	if buf.Len() != 0 {
		t.Fatalf("info should be filtered at warn level, got %q", buf.String())
	}
	logger.Warn("yes")
	if !strings.Contains(buf.String(), "yes") {
		t.Fatal("warn should pass")
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"debug", "DEBUG"},
		{"INFO", "INFO"},
		{"warn", "WARN"},
		{"ERROR", "ERROR"},
		{"bogus", "INFO"},
		{"", "INFO"},
	}
	for _, c := range cases {
		got := parseLevel(c.in)
		if got.String() != c.want {
			t.Fatalf("parseLevel(%q) = %s, want %s", c.in, got.String(), c.want)
		}
	}
}
