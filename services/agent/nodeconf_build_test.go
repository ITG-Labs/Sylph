package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const testBetaKey = "MDHqaJPhoMxwOUVfWKd2y8WK8XFd8mmQr0LHNlU2ZVE"

func goldenInbounds() []Inbound {
	alpha := testInbound("alpha")
	alpha.Reality.ServerNames = []string{"www.alpha.example", "alpha.example"}
	alpha.Reality.Dest = "www.alpha.example:443"
	beta := testInbound("beta")
	beta.Reality.ServerNames = []string{"beta.example"}
	beta.Reality.Dest = "beta.example:8443"
	beta.Reality.ShortIDs = []string{"0a1b", "c2d3e4f5"}
	beta.Reality.PrivateKey = testBetaKey
	return []Inbound{beta, alpha}
}

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from golden:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestBuildXrayMatchesGolden(t *testing.T) {
	got, err := BuildXray(goldenInbounds(), "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "xray_two_inbounds.json", got)
}

func TestBuildXrayFrameWithoutInbounds(t *testing.T) {
	for _, in := range [][]Inbound{nil, {}} {
		got, err := BuildXray(in, "127.0.0.1:10085")
		if err != nil {
			t.Fatal(err)
		}
		assertGolden(t, "xray_frame.json", got)
	}
}

func TestBuildCaddyMatchesGolden(t *testing.T) {
	got, err := BuildCaddy(goldenInbounds(), "127.0.0.1:2019")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "caddy_two_inbounds.json", got)
}

func TestBuildCaddyWithoutInboundsListensNowhere(t *testing.T) {
	got, err := BuildCaddy([]Inbound{}, "127.0.0.1:2019")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{\n  \"admin\": {\n    \"listen\": \"127.0.0.1:2019\"\n  }\n}\n" {
		t.Fatalf("empty caddy config = %s", got)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	in := goldenInbounds()
	reversed := []Inbound{in[1], in[0]}
	for _, build := range []func([]Inbound) ([]byte, error){
		func(in []Inbound) ([]byte, error) { return BuildXray(in, "127.0.0.1:10085") },
		func(in []Inbound) ([]byte, error) { return BuildCaddy(in, "127.0.0.1:2019") },
	} {
		a, err := build(in)
		if err != nil {
			t.Fatal(err)
		}
		b, err := build(reversed)
		if err != nil {
			t.Fatal(err)
		}
		c, err := build(goldenInbounds())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) || !bytes.Equal(a, c) {
			t.Fatalf("same inbounds built differently:\n%s\n%s", a, b)
		}
	}
	if in[0].Tag != "beta" {
		t.Fatal("build reordered the caller's slice")
	}
}

func TestCaddyRoutesPointAtXrayInbounds(t *testing.T) {
	in := append(goldenInbounds(), testInbound("gamma"))
	xr, err := BuildXray(in, "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	cd, err := BuildCaddy(in, "127.0.0.1:2019")
	if err != nil {
		t.Fatal(err)
	}
	var x xrayConfig
	if err := json.Unmarshal(xr, &x); err != nil {
		t.Fatal(err)
	}
	var c caddyConfig
	if err := json.Unmarshal(cd, &c); err != nil {
		t.Fatal(err)
	}
	routes := c.Apps.Layer4.Servers["p443"].Routes
	if len(routes) != len(x.Inbounds)+1 {
		t.Fatalf("routes = %d, want one per inbound plus fallback", len(routes))
	}
	for i, xin := range x.Inbounds {
		r := routes[i]
		dial := r.Handle[0].Upstreams[0].Dial[0]
		if !reflect.DeepEqual(r.Match[0].TLS.SNI, xin.StreamSettings.RealitySettings.ServerNames) || dial != "127.0.0.1:"+strconv.Itoa(xin.Port) {
			t.Fatalf("route %d = sni %v -> %s, xray inbound %s listens %d for %v", i, r.Match[0].TLS.SNI, dial, xin.Tag, xin.Port, xin.StreamSettings.RealitySettings.ServerNames)
		}
		if r.Handle[0].ProxyProtocol != "v2" || !xin.StreamSettings.Sockopt.AcceptProxyProtocol || xin.Listen != "127.0.0.1" {
			t.Fatalf("inbound %s is reachable without PROXY protocol", xin.Tag)
		}
	}
	last := routes[len(routes)-1]
	if last.Match != nil || last.Handle[0].Upstreams[0].Dial[0] != "127.0.0.1:"+strconv.Itoa(x.Inbounds[0].Port) {
		t.Fatalf("fallback route = %+v, want unmatched route to the first inbound", last)
	}
}

func TestBuildRejectsInboundsItCannotServe(t *testing.T) {
	mutate := func(f func(*Inbound)) []Inbound {
		in := testInbound("main")
		r := *in.Reality
		in.Reality = &r
		f(&in)
		return []Inbound{in}
	}
	cases := map[string][]Inbound{
		"protocol":        mutate(func(in *Inbound) { in.Protocol = "vmess" }),
		"transport":       mutate(func(in *Inbound) { in.Transport = "xhttp" }),
		"security":        mutate(func(in *Inbound) { in.Security = "tls" }),
		"no reality":      mutate(func(in *Inbound) { in.Reality = nil }),
		"no key":          mutate(func(in *Inbound) { in.Reality.PrivateKey = "" }),
		"no server names": mutate(func(in *Inbound) { in.Reality.ServerNames = nil }),
		"no short ids":    mutate(func(in *Inbound) { in.Reality.ShortIDs = nil }),
		"no dest":         mutate(func(in *Inbound) { in.Reality.Dest = "" }),
		"port":            mutate(func(in *Inbound) { in.Port = 0 }),
		"api tag":         mutate(func(in *Inbound) { in.Tag = xrayAPITag }),
		"upper tag":       mutate(func(in *Inbound) { in.Tag = "Main" }),
		"duplicate tag":   {testInbound("main"), testInbound("main")},
		"shared sni": append(testInbounds("main"), func() Inbound {
			o := testInbound("other")
			o.Reality.ServerNames = []string{"main.example.com"}
			return o
		}()),
		"shared sni in one": mutate(func(in *Inbound) { in.Reality.ServerNames = []string{"a.example", "a.example"} }),
	}
	for name, in := range cases {
		for _, build := range []func([]Inbound) ([]byte, error){
			func(in []Inbound) ([]byte, error) { return BuildXray(in, "127.0.0.1:10085") },
			func(in []Inbound) ([]byte, error) { return BuildCaddy(in, "127.0.0.1:2019") },
		} {
			_, err := build(in)
			if err == nil {
				t.Fatalf("%s: accepted", name)
			}
			if strings.Contains(err.Error(), testPrivateKey) {
				t.Fatalf("%s: error leaks the private key: %v", name, err)
			}
		}
	}
}

func TestBuildRejectsDestInsideTheNode(t *testing.T) {
	for _, dest := range []string{
		"127.0.0.1:2019", "127.8.8.8:443", "10.0.0.5:443", "172.18.0.2:443", "192.168.1.1:443", "100.64.1.1:443",
		"169.254.169.254:80", "0.0.0.0:443", "[::1]:10085", "[::ffff:127.0.0.1]:443", "[fe80::1%eth0]:443", "[fd00::1]:443",
		"localhost:2019", "LOCALHOST.:443", "xray:10085", "caddy:2019", "admin.internal:443", "router.lan:443",
		"printer.local:443", "app.localhost:443", "nas.home.arpa:443", "www.example.com", "",
		"127.1:2019", "0x7f.1:2019", "0177.0.0.1:443", "127.0.0.1.:443", "10.1:443", "a..:443",
	} {
		in := testInbound("main")
		in.Reality.Dest = dest
		for _, build := range []func([]Inbound) ([]byte, error){
			func(in []Inbound) ([]byte, error) { return BuildXray(in, "127.0.0.1:10085") },
			func(in []Inbound) ([]byte, error) { return BuildCaddy(in, "127.0.0.1:2019") },
		} {
			if _, err := build([]Inbound{in}); err == nil {
				t.Fatalf("dest %q accepted", dest)
			}
		}
	}
	for _, dest := range []string{"www.example.com:443", "1.1.1.1:443", "[2606:4700::1111]:443", "dest.test:28443", "WWW.Example.COM.:443"} {
		in := testInbound("main")
		in.Reality.Dest = dest
		if _, err := BuildXray([]Inbound{in}, "127.0.0.1:10085"); err != nil {
			t.Fatalf("public dest %q rejected: %v", dest, err)
		}
	}
}
