package netaddr

import "testing"

func TestInternal(t *testing.T) {
	for _, h := range []string{"localhost", "a.localhost", "ip6-loopback", "127.0.0.1", "10.1.2.3", "192.168.0.1", "172.16.5.5",
		"169.254.169.254", "100.64.0.1", "0.0.0.0", "255.255.255.255", "::1", "fe80::1", "fc00::1", "::ffff:10.0.0.1", "64:ff9b::a00:1", "224.0.0.1"} {
		if !Internal(h) {
			t.Errorf("%s is not internal", h)
		}
	}
	for _, h := range []string{"1.1.1.1", "example.com", "2606:4700::1111", "::ffff:8.8.8.8"} {
		if Internal(h) {
			t.Errorf("%s is internal", h)
		}
	}
}

func TestPublicHost(t *testing.T) {
	for _, h := range []string{"example.com", "Example.COM.", "1.1.1.1", "2606:4700::1111", "engage.cloudflareclient.com"} {
		if !PublicHost(h) {
			t.Errorf("%s is not public", h)
		}
	}
	for _, h := range []string{"", "localhost", "xray", "core", "127.1", "2130706433", "0x7f000001", "10.0.0.1", "169.254.169.254",
		"metadata.google.internal", "nas.local", "router.lan", "box.home.arpa", "srv.localdomain", "a b.com", "::1", "-bad.com"} {
		if PublicHost(h) {
			t.Errorf("%s is public", h)
		}
	}
}

func TestCanonicalHost(t *testing.T) {
	if got := CanonicalHost("2001:DB8:0::1"); got != "2001:db8::1" {
		t.Fatalf("got %s", got)
	}
	if got := CanonicalHost("::ffff:1.2.3.4"); got != "1.2.3.4" {
		t.Fatalf("got %s", got)
	}
	if got := CanonicalHost("Example.COM"); got != "example.com" {
		t.Fatalf("got %s", got)
	}
}
