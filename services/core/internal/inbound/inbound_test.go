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

func TestShortIDsAreLowercasedAndUnique(t *testing.T) {
	s := validSpec()
	s.Reality.ShortIDs = []string{"ABCD", "0f"}
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	if s.Reality.ShortIDs[0] != "abcd" || s.Reality.ShortIDs[1] != "0f" {
		t.Fatalf("short ids = %v", s.Reality.ShortIDs)
	}
	s = validSpec()
	s.Reality.ShortIDs = []string{"ABCD", "abcd"}
	if f := fieldOf(t, Normalize(&s)); f != "reality.short_ids" {
		t.Fatalf("duplicate short ids: field %s", f)
	}
}

func TestDestRejectsInternalAddresses(t *testing.T) {
	for _, dest := range []string{
		"127.0.0.1:443", "10.0.0.1:443", "192.168.1.1:443", "172.16.0.1:443", "169.254.1.1:443", "100.64.0.1:443",
		"0.0.0.0:443", "224.0.0.1:443", "255.255.255.255:443", "[::1]:443", "[fd00::1]:443", "[fe80::1]:443", "[::]:443",
		"[::ffff:127.0.0.1]:443", "localhost:443", "foo.localhost:443", "LOCALHOST:443",
	} {
		s := validSpec()
		s.Reality.Dest = dest
		if err := Normalize(&s); err == nil || fieldOf(t, err) != "reality.dest" {
			t.Errorf("dest %s accepted: %v", dest, err)
		}
	}
	for _, dest := range []string{"www.microsoft.com:443", "1.1.1.1:443", "[2606:4700::1111]:443"} {
		s := validSpec()
		s.Reality.Dest = dest
		if err := Normalize(&s); err != nil {
			t.Errorf("dest %s rejected: %v", dest, err)
		}
	}
}

func TestLabelRejectsControlCharacters(t *testing.T) {
	for _, label := range []string{"a\x07b", "line break", "tab\t"} {
		s := validSpec()
		s.Label = label
		if f := fieldOf(t, Normalize(&s)); f != "label" {
			t.Fatalf("label %q: field %s", label, f)
		}
	}
	s := validSpec()
	s.Label = "   "
	if f := fieldOf(t, Normalize(&s)); f != "label" {
		t.Fatalf("label of spaces: field %s", f)
	}
}

func TestDestHost(t *testing.T) {
	for in, want := range map[string]string{"WWW.Example.com:443": "www.example.com", "[2606:4700::1111]:443": "2606:4700::1111", "bad": ""} {
		if got := DestHost(in); got != want {
			t.Errorf("DestHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDestRejectsNumericShorthandsThatResolversTurnIntoIPs(t *testing.T) {
	for _, dest := range []string{
		"127.1:443", "2130706433:443", "0x7f000001:443", "0x7f.0.0.1:443", "0177.0.0.1:443", "127.0.0.01:443",
		"10.1:443", "192.168.1:443", "1.1:443",
		"localhost.localdomain:443", "ip6-localhost:443", "ip6-loopback:443",
		"[::127.0.0.1]:443", "[::ffff:0:127.0.0.1]:443", "[64:ff9b::7f00:1]:443", "[64:ff9b::a00:1]:443",
	} {
		s := validSpec()
		s.Reality.Dest = dest
		if err := Normalize(&s); err == nil || fieldOf(t, err) != "reality.dest" {
			t.Errorf("dest %s accepted: %v", dest, err)
		}
	}
	for _, dest := range []string{"decoy:443", "mask.test:443", "xn--80ak6aa92e.com:443", "[64:ff9b::808:808]:443"} {
		s := validSpec()
		s.Reality.Dest = dest
		if err := Normalize(&s); err != nil {
			t.Errorf("dest %s rejected: %v", dest, err)
		}
	}
}

func TestCanonicalHostMatchesEveryWayToWriteAnIP(t *testing.T) {
	for _, pair := range [][2]string{
		{"2001:DB8:0::1", "2001:db8::1"}, {"::ffff:203.0.113.7", "203.0.113.7"}, {"WWW.Example.COM", "www.example.com"},
	} {
		if CanonicalHost(pair[0]) != CanonicalHost(pair[1]) {
			t.Errorf("%s and %s differ: %q vs %q", pair[0], pair[1], CanonicalHost(pair[0]), CanonicalHost(pair[1]))
		}
	}
	if DestHost("[2001:DB8:0::1]:443") != CanonicalHost("2001:db8::1") {
		t.Fatalf("DestHost is not canonical: %q", DestHost("[2001:DB8:0::1]:443"))
	}
}

func TestDestPortIsStoredInItsUsualForm(t *testing.T) {
	s := validSpec()
	s.Reality.Dest = "www.example.com:+0443"
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	if s.Reality.Dest != "www.example.com:443" {
		t.Fatalf("dest = %q", s.Reality.Dest)
	}
}
