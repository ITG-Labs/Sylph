package inbound

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func validSpec() Spec {
	return Spec{
		Tag:       "reality-tcp",
		Port:      443,
		Protocol:  "vless",
		Transport: "tcp",
		Security:  "reality",
		Label:     "REALITY",
		Reality: &Reality{
			Dest:        "www.example.com:443",
			ServerNames: []string{"www.example.com"},
			ShortIDs:    []string{"0123456789abcdef"},
			Fingerprint: "chrome",
			PrivateKey:  "",
		},
	}
}

func fieldOf(t *testing.T, err error) string {
	t.Helper()
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want *FieldError", err)
	}
	return fe.Field
}

func TestNormalizeGeneratesKeysAndShortID(t *testing.T) {
	s := validSpec()
	s.Reality.ShortIDs = nil
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s.Reality.PrivateKey)
	if err != nil || len(raw) != 32 {
		t.Fatalf("private key %q: %v", s.Reality.PrivateKey, err)
	}
	if len(s.Reality.ShortIDs) != 1 || len(s.Reality.ShortIDs[0]) != 16 {
		t.Fatalf("short ids = %v", s.Reality.ShortIDs)
	}
	pub, err := PublicKey(s.Reality.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	if pub != base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()) {
		t.Fatalf("public key mismatch")
	}
}

func TestNormalizeKeepsGivenKeyAndShortIDs(t *testing.T) {
	s := validSpec()
	key := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	s.Reality.PrivateKey = key
	s.Reality.ShortIDs = []string{"ab", "0123456789abcdef"}
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	if s.Reality.PrivateKey != key || len(s.Reality.ShortIDs) != 2 {
		t.Fatalf("spec changed: %+v", s.Reality)
	}
}

func TestNormalizeDefaults(t *testing.T) {
	s := validSpec()
	s.Protocol, s.Transport, s.Security, s.Reality.Fingerprint = "", "", "", ""
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	if s.Protocol != "vless" || s.Transport != "tcp" || s.Security != "reality" || s.Reality.Fingerprint != "chrome" {
		t.Fatalf("defaults = %+v / %+v", s, s.Reality)
	}
}

func TestNormalizeLowercasesHosts(t *testing.T) {
	s := validSpec()
	s.Reality.ServerNames = []string{"WWW.Example.COM"}
	s.Reality.Dest = "WWW.Example.COM:443"
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	if s.Reality.ServerNames[0] != "www.example.com" || s.Reality.Dest != "www.example.com:443" {
		t.Fatalf("not lowercased: %+v", s.Reality)
	}
}

func TestNormalizeRejects(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*Spec)
		field string
	}{
		{"empty tag", func(s *Spec) { s.Tag = "" }, "tag"},
		{"upper tag", func(s *Spec) { s.Tag = "Main" }, "tag"},
		{"long tag", func(s *Spec) { s.Tag = strings.Repeat("a", 33) }, "tag"},
		{"tag with space", func(s *Spec) { s.Tag = "a b" }, "tag"},
		{"port 8443", func(s *Spec) { s.Port = 8443 }, "port"},
		{"protocol trojan", func(s *Spec) { s.Protocol = "trojan" }, "protocol"},
		{"transport xhttp", func(s *Spec) { s.Transport = "xhttp" }, "transport"},
		{"security tls", func(s *Spec) { s.Security = "tls" }, "security"},
		{"long label", func(s *Spec) { s.Label = strings.Repeat("я", 33) }, "label"},
		{"no reality", func(s *Spec) { s.Reality = nil }, "reality"},
		{"dest without port", func(s *Spec) { s.Reality.Dest = "www.example.com" }, "reality.dest"},
		{"dest bad port", func(s *Spec) { s.Reality.Dest = "www.example.com:0" }, "reality.dest"},
		{"dest bad host", func(s *Spec) { s.Reality.Dest = "-bad-.com:443" }, "reality.dest"},
		{"no server names", func(s *Spec) { s.Reality.ServerNames = nil }, "reality.server_names"},
		{"ip server name", func(s *Spec) { s.Reality.ServerNames = []string{"1.2.3.4"} }, "reality.server_names"},
		{"dotless server name", func(s *Spec) { s.Reality.ServerNames = []string{"localhost"} }, "reality.server_names"},
		{"duplicate server name", func(s *Spec) { s.Reality.ServerNames = []string{"a.example.com", "A.example.com"} }, "reality.server_names"},
		{"odd short id", func(s *Spec) { s.Reality.ShortIDs = []string{"abc"} }, "reality.short_ids"},
		{"empty short id", func(s *Spec) { s.Reality.ShortIDs = []string{""} }, "reality.short_ids"},
		{"long short id", func(s *Spec) { s.Reality.ShortIDs = []string{"0123456789abcdef01"} }, "reality.short_ids"},
		{"non hex short id", func(s *Spec) { s.Reality.ShortIDs = []string{"zz"} }, "reality.short_ids"},
		{"unknown fingerprint", func(s *Spec) { s.Reality.Fingerprint = "netscape" }, "reality.fingerprint"},
		{"bad private key", func(s *Spec) { s.Reality.PrivateKey = "short" }, "reality.private_key"},
		{"padded private key", func(s *Spec) {
			s.Reality.PrivateKey = base64.URLEncoding.EncodeToString(make([]byte, 32))
		}, "reality.private_key"},
	}
	for _, c := range cases {
		s := validSpec()
		c.edit(&s)
		if got := fieldOf(t, Normalize(&s)); got != c.field {
			t.Errorf("%s: field = %q, want %q", c.name, got, c.field)
		}
	}
}

func TestFieldErrorDoesNotEchoPrivateKey(t *testing.T) {
	s := validSpec()
	s.Reality.PrivateKey = "secret-key-value-that-is-wrong"
	err := Normalize(&s)
	if err == nil || strings.Contains(err.Error(), "secret-key-value") {
		t.Fatalf("err = %v", err)
	}
}

func TestFlow(t *testing.T) {
	if got := Flow("tcp", "reality"); got != "xtls-rprx-vision" {
		t.Fatalf("tcp+reality flow = %q", got)
	}
	if got := Flow("xhttp", "reality"); got != "" {
		t.Fatalf("xhttp flow = %q, want empty", got)
	}
}

func TestPublicKeyRejectsGarbage(t *testing.T) {
	if _, err := PublicKey("not base64!"); err == nil {
		t.Fatal("want error")
	}
}

func TestServerNamesOf(t *testing.T) {
	s := validSpec()
	s.Reality.ServerNames = []string{"a.example.com", "b.example.com"}
	got := ServerNames(&s)
	if len(got) != 2 || got[0] != "a.example.com" {
		t.Fatalf("server names = %v", got)
	}
	s.Reality = nil
	if ServerNames(&s) != nil {
		t.Fatal("no reality must give nil")
	}
}

func TestNewPrivateKeyIsClampedLikeXray(t *testing.T) {
	for i := 0; i < 50; i++ {
		key, err := NewPrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(key)
		if err != nil || len(raw) != 32 {
			t.Fatalf("key %q: %v", key, err)
		}
		if raw[0]&7 != 0 || raw[31]&128 != 0 || raw[31]&64 == 0 {
			t.Fatalf("key %q is not clamped", key)
		}
	}
}
