package panel

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := hashPassword("correct horse", []byte("0123456789abcdef"), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$1000$") {
		t.Fatalf("unexpected format %q", h)
	}
	ph, err := parsePasswordHash(h)
	if err != nil {
		t.Fatal(err)
	}
	if !ph.verify("correct horse") {
		t.Fatal("right password rejected")
	}
	if ph.verify("correct horsf") || ph.verify("") || ph.verify("correct horse ") {
		t.Fatal("wrong password accepted")
	}
}

func TestPasswordHashDefaultCost(t *testing.T) {
	h, err := HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	ph, err := parsePasswordHash(h)
	if err != nil {
		t.Fatal(err)
	}
	if ph.iter != defaultIterations || len(ph.salt) != 16 || len(ph.key) != 32 {
		t.Fatalf("cost %d salt %d key %d", ph.iter, len(ph.salt), len(ph.key))
	}
	h2, _ := HashPassword("pw")
	if h == h2 {
		t.Fatal("salt is not random")
	}
}

func TestParsePasswordHashRejectsGarbage(t *testing.T) {
	good, _ := hashPassword("pw", []byte("0123456789abcdef"), 1000)
	parts := strings.Split(good, "$")
	for _, bad := range []string{
		"",
		"plain",
		"bcrypt$1000$" + parts[2] + "$" + parts[3],
		"pbkdf2-sha256$abc$" + parts[2] + "$" + parts[3],
		"pbkdf2-sha256$0$" + parts[2] + "$" + parts[3],
		"pbkdf2-sha256$1000$!!$" + parts[3],
		"pbkdf2-sha256$1000$" + parts[2] + "$!!",
		"pbkdf2-sha256$1000$$" + parts[3],
		"pbkdf2-sha256$1000$" + parts[2] + "$",
		good + "$extra",
	} {
		if _, err := parsePasswordHash(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
