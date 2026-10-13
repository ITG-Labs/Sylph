package panel

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAdmins(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "admins.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewAdminRecordLoads(t *testing.T) {
	rec, password, uri, err := NewAdmin("Иван")
	if err != nil {
		t.Fatal(err)
	}
	if len(password) < 20 {
		t.Fatalf("password too short: %d", len(password))
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("uri %q", uri)
	}
	q := u.Query()
	if q.Get("secret") != rec.TOTPSecret || q.Get("issuer") != "Sylph" || q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Fatalf("uri params %v", q)
	}
	if u.Path != "/Sylph:Иван" {
		t.Fatalf("label %q", u.Path)
	}
	b, _ := json.Marshal([]AdminRecord{rec})
	admins, err := LoadAdmins(writeAdmins(t, string(b)))
	if err != nil {
		t.Fatal(err)
	}
	a := admins["Иван"]
	if a == nil || !a.password.verify(password) || len(a.secret) != 20 {
		t.Fatalf("loaded admin %+v", a)
	}
}

func TestLoadAdminsRejectsBadFiles(t *testing.T) {
	good, _ := hashPassword("pw", []byte("0123456789abcdef"), minIterations)
	weak, _ := hashPassword("pw", []byte("0123456789abcdef"), minIterations-1)
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	rec := func(name, hash, sec string) string {
		b, _ := json.Marshal(map[string]string{"name": name, "password_hash": hash, "totp_secret": sec})
		return string(b)
	}
	ok := rec("Иван", good, secret)
	if _, err := LoadAdmins(writeAdmins(t, "["+ok+"]")); err != nil {
		t.Fatalf("good file rejected: %v", err)
	}
	for name, body := range map[string]string{
		"empty list":     "[]",
		"not json":       "{",
		"object":         ok,
		"unknown field":  `[{"name":"a","password_hash":"` + good + `","totp_secret":"` + secret + `","role":"x"}]`,
		"duplicate":      "[" + ok + "," + ok + "]",
		"empty name":     "[" + rec("", good, secret) + "]",
		"spaces":         "[" + rec(" Иван", good, secret) + "]",
		"control":        "[" + rec("Ив\u0007ан", good, secret) + "]",
		"line separator": "[" + rec("Ив ан", good, secret) + "]",
		"too long":       "[" + rec(strings.Repeat("я", 65), good, secret) + "]",
		"bad hash":       "[" + rec("Иван", "plain", secret) + "]",
		"weak hash":      "[" + rec("Иван", weak, secret) + "]",
		"bad secret":     "[" + rec("Иван", good, "GEZDGNBV") + "]",
		"trailing data":  "[" + ok + "] []",
	} {
		if _, err := LoadAdmins(writeAdmins(t, body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := LoadAdmins(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("missing file accepted")
	}
	if _, err := LoadAdmins(writeAdmins(t, "["+rec(strings.Repeat("я", 64), good, secret)+"]")); err != nil {
		t.Errorf("64 runes rejected: %v", err)
	}
}
