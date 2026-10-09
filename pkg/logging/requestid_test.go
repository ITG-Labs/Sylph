package logging

import (
	"context"
	"testing"
)

func TestNewRequestIDFormat(t *testing.T) {
	id := NewRequestID()
	if len(id) != 36 {
		t.Fatalf("len = %d, want 36", len(id))
	}
	if id[14] != '4' {
		t.Fatalf("version nibble = %q, want '4'", id[14])
	}
	switch id[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Fatalf("variant = %q, want 8/9/a/b", id[19])
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				t.Fatalf("pos %d = %q, want '-'", i, c)
			}
			continue
		}
		isHex := c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
		if !isHex {
			t.Fatalf("pos %d = %q, not hex", i, c)
		}
	}
}

func TestNewRequestIDUnique(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := NewRequestID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestRequestIDContext(t *testing.T) {
	ctx := context.Background()
	if RequestIDFromContext(ctx) != "" {
		t.Fatal("bare context should have empty request id")
	}
	ctx = WithRequestID(ctx, "abc")
	if RequestIDFromContext(ctx) != "abc" {
		t.Fatalf("got %q, want %q", RequestIDFromContext(ctx), "abc")
	}
}
