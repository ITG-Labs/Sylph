package model

import "testing"

func TestNewUUIDFormat(t *testing.T) {
	id := NewUUID()
	if len(id) != 36 {
		t.Fatalf("len = %d, want 36", len(id))
	}
	if id[14] != '4' {
		t.Fatalf("version nibble = %q", id[14])
	}
	switch id[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Fatalf("variant = %q", id[19])
	}
}

func TestNewTokenFormat(t *testing.T) {
	tok := NewToken()
	if len(tok) != 32 {
		t.Fatalf("len = %d, want 32", len(tok))
	}
	for _, c := range tok {
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
		if !ok {
			t.Fatalf("char %q not hex", c)
		}
	}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		tk := NewToken()
		if seen[tk] {
			t.Fatalf("dup %q", tk)
		}
		seen[tk] = true
	}
}

func TestNewInviteCodeFormat(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		c := NewInviteCode()
		if !ValidInviteCode(c) {
			t.Fatalf("invalid code %q", c)
		}
		if seen[c] {
			t.Fatalf("dup %q", c)
		}
		seen[c] = true
	}
	for _, bad := range []string{"", "short", "AAAAAAAAAAAAAAAAAAAAA+", "AAAAAAAAAAAAAAAAAAAAAAA", "AAAAAAAAAAAAAAAAAAAA A"} {
		if ValidInviteCode(bad) {
			t.Fatalf("accepted %q", bad)
		}
	}
}
