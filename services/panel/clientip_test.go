package panel

import (
	"net/http/httptest"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	tr, err := parseTrustedProxies("")
	if err != nil || len(tr) != 2 {
		t.Fatalf("default: %v %v", tr, err)
	}
	tr, err = parseTrustedProxies(" 10.0.0.0/8, 172.16.5.4/32 ,fd00::/8")
	if err != nil || len(tr) != 3 {
		t.Fatalf("list: %v %v", tr, err)
	}
	for _, bad := range []string{"10.0.0.1", "10.0.0.0/33", "nope", "10.0.0.0/8,,"} {
		if _, err := parseTrustedProxies(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestClientIP(t *testing.T) {
	tr, _ := parseTrustedProxies("127.0.0.1/32,::1/128,172.18.0.0/16")
	for _, tc := range []struct {
		remote string
		xff    []string
		want   string
	}{
		{"203.0.113.9:5555", nil, "203.0.113.9"},
		{"203.0.113.9:5555", []string{"198.51.100.1"}, "203.0.113.9"},
		{"127.0.0.1:5555", nil, "127.0.0.1"},
		{"127.0.0.1:5555", []string{"198.51.100.1"}, "198.51.100.1"},
		{"127.0.0.1:5555", []string{"6.6.6.6, 198.51.100.1"}, "198.51.100.1"},
		{"127.0.0.1:5555", []string{"6.6.6.6", "198.51.100.1"}, "198.51.100.1"},
		{"172.18.0.3:5555", []string{"6.6.6.6, 198.51.100.1, 172.18.0.2"}, "198.51.100.1"},
		{"127.0.0.1:5555", []string{"garbage, 172.18.0.2"}, "172.18.0.2"},
		{"127.0.0.1:5555", []string{"garbage"}, "127.0.0.1"},
		{"[::1]:5555", []string{"2001:db8::1"}, "2001:db8::1"},
		{"[::ffff:127.0.0.1]:5555", []string{"198.51.100.1"}, "198.51.100.1"},
		{"127.0.0.1:5555", []string{"::ffff:198.51.100.1"}, "198.51.100.1"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		for _, v := range tc.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := tr.clientIP(r); got != tc.want {
			t.Errorf("%s %v: got %s want %s", tc.remote, tc.xff, got, tc.want)
		}
	}
}
