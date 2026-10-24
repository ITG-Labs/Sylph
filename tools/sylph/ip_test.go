package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/envfile"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
)

func TestParseEgressIPs(t *testing.T) {
	got, err := parseEgressIPs(" 203.0.113.7, 198.51.100.4 ,")
	if err != nil || strings.Join(got, " ") != "203.0.113.7 198.51.100.4" {
		t.Fatalf("parse = %v %v", got, err)
	}
	if got, err := parseEgressIPs(""); err != nil || got != nil {
		t.Fatalf("empty = %v %v", got, err)
	}
	for _, bad := range []string{"2001:db8::1", "203.0.113.07", "example.com", "127.0.0.1", "0.0.0.0", "169.254.1.1", "203.0.113.7,203.0.113.7"} {
		if _, err := parseEgressIPs(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	many := make([]string, maxEgressIPs+1)
	for i := range many {
		many[i] = "10.0.0." + string(rune('0'+i%10)) + string(rune('0'+i/10))
	}
	if _, err := parseEgressIPs(strings.Join(many, ",")); err == nil {
		t.Fatal("more than the maximum accepted")
	}
}

func TestRenderEgressOverride(t *testing.T) {
	if renderEgressOverride(nil) != nil {
		t.Fatal("override without addresses")
	}
	got := string(renderEgressOverride([]string{"203.0.113.7", "198.51.100.4"}))
	for _, want := range []string{
		"      default:\n        gw_priority: 100\n",
		"      egress-203-0-113-7:\n        interface_name: egress1\n",
		"      egress-198-51-100-4:\n        interface_name: egress2\n",
		"      SYLPH_EGRESS: 203.0.113.7=egress1,198.51.100.4=egress2\n",
		"  egress-203-0-113-7:\n    driver: bridge\n    driver_opts:\n      com.docker.network.host_ipv4: 203.0.113.7\n",
		"  egress-198-51-100-4:\n    driver: bridge\n    driver_opts:\n      com.docker.network.host_ipv4: 198.51.100.4\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("override lacks %q:\n%s", want, got)
		}
	}
}

func nodeEnv(t *testing.T, ips string) (*role.Role, *envfile.Env) {
	t.Helper()
	r, err := role.Get("node")
	if err != nil {
		t.Fatal(err)
	}
	env, _, err := r.Template()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := env.Lookup(keyEgressIPs); !ok {
		t.Fatalf("%s is missing from the node .env.example", keyEgressIPs)
	}
	env.Set(keyEgressIPs, ips)
	return r, env
}

func TestWriteEgressOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, overrideFile)
	r, env := nodeEnv(t, "203.0.113.7")
	if err := writeEgressOverride(dir, r, env); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), "host_ipv4: 203.0.113.7") {
		t.Fatalf("override = %s %v", data, err)
	}
	env.Set(keyEgressIPs, "")
	if err := writeEgressOverride(dir, r, env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("override left without addresses: %v", err)
	}
	if err := writeEgressOverride(dir, r, env); err != nil {
		t.Fatalf("removing a missing override: %v", err)
	}
	env.Set(keyEgressIPs, "nope")
	if err := writeEgressOverride(dir, r, env); err == nil {
		t.Fatal("bad value accepted")
	}
	panel, err := role.Get("panel")
	if err != nil {
		t.Fatal(err)
	}
	pe := envfile.New()
	pe.Set(keyEgressIPs, "203.0.113.7")
	if err := writeEgressOverride(dir, panel, pe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("override written for a role that is not a node")
	}
}

func TestNodeEnvKeepsEgressIPs(t *testing.T) {
	dir := t.TempDir()
	r, env := nodeEnv(t, "203.0.113.7,198.51.100.4")
	if err := writeEnv(dir, r, env); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	back, err := envfile.Parse(data)
	if err != nil || back.Get(keyEgressIPs) != "203.0.113.7,198.51.100.4" {
		t.Fatalf(".env = %s %v", data, err)
	}
}

func fakeHost(t *testing.T, main string, addrs ...string) {
	t.Helper()
	prevAddrs, prevMain, prevProbe, prevWait, prevVersions := hostAddrs, mainSource, probeEgress, probeWait, dockerVersions
	t.Cleanup(func() {
		hostAddrs, mainSource, probeEgress, probeWait, dockerVersions = prevAddrs, prevMain, prevProbe, prevWait, prevVersions
	})
	dockerVersions = func(context.Context) (string, string, error) { return "29.9.2", "5.6.1", nil }
	hostAddrs = func() ([]net.Addr, error) {
		var out []net.Addr
		for _, a := range append([]string{main + "/24"}, addrs...) {
			ip, n, err := net.ParseCIDR(a)
			if err != nil {
				t.Fatal(err)
			}
			n.IP = ip
			out = append(out, n)
		}
		return out, nil
	}
	mainSource = func() (string, error) { return main, nil }
	probeWait = time.Millisecond
}

func TestEgressChecks(t *testing.T) {
	ctx := context.Background()
	u := &ui.UI{Out: &bytes.Buffer{}}
	fakeHost(t, "203.0.113.2", "203.0.113.7/32")

	calls := 0
	probeEgress = func(ctx context.Context, ip string) (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("i/o timeout")
		}
		return ip, nil
	}
	if err := egressChecks(ctx, u, "203.0.113.7", false); err != nil || calls != 3 {
		t.Fatalf("checks with a slow hoster = %v after %d probes", err, calls)
	}
	if err := egressChecks(ctx, u, "203.0.113.9", false); err == nil || !strings.Contains(err.Error(), "нет на интерфейсах") {
		t.Fatalf("address not on the host: %v", err)
	}
	if err := egressChecks(ctx, u, "203.0.113.2", false); err == nil || !strings.Contains(err.Error(), "основной адрес") {
		t.Fatalf("main address: %v", err)
	}
	probeEgress = func(ctx context.Context, ip string) (string, error) { return "203.0.113.2", nil }
	if err := egressChecks(ctx, u, "203.0.113.7", false); err == nil || !strings.Contains(err.Error(), "подменяет") {
		t.Fatalf("hoster rewrites the source: %v", err)
	}
	calls = 0
	probeEgress = func(ctx context.Context, ip string) (string, error) { calls++; return "", errors.New("i/o timeout") }
	if err := egressChecks(ctx, u, "203.0.113.7", false); err == nil || calls != probeTries {
		t.Fatalf("dead address = %v after %d probes", err, calls)
	}
	calls = 0
	if err := egressChecks(ctx, u, "203.0.113.7", true); err != nil || calls != 0 {
		t.Fatalf("--no-check = %v, probes %d", err, calls)
	}
	for _, v := range [][2]string{{"28.0.4", "5.6.1"}, {"29.9.2", "2.35.1"}} {
		dockerVersions = func(context.Context) (string, string, error) { return v[0], v[1], nil }
		if err := egressChecks(ctx, u, "203.0.113.7", true); err == nil || !strings.Contains(err.Error(), "старый") {
			t.Fatalf("docker %s, compose %s accepted: %v", v[0], v[1], err)
		}
	}
}

func TestEgressOverrideMergesWithNodeCompose(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	dir := t.TempDir()
	r, env := nodeEnv(t, "203.0.113.7")
	env.Set("SYLPH_CORE_URL", "https://203.0.113.10:443")
	if _, err := writeDeployFiles(dir, r); err != nil {
		t.Fatal(err)
	}
	if err := writeEnv(dir, r, env); err != nil {
		t.Fatal(err)
	}
	if err := writeEgressOverride(dir, r, env); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("docker", "compose", "config", "--format", "json")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("compose config: %v", err)
	}
	var cfg struct {
		Services map[string]struct {
			NetworkMode string `json:"network_mode"`
			Environment map[string]string
			Networks    map[string]*struct {
				GwPriority    int    `json:"gw_priority"`
				InterfaceName string `json:"interface_name"`
			}
		}
		Networks map[string]struct {
			DriverOpts map[string]string `json:"driver_opts"`
		}
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	caddy := cfg.Services["caddy"]
	if caddy.Networks["default"] == nil || caddy.Networks["default"].GwPriority != 100 ||
		caddy.Networks["egress-203-0-113-7"] == nil || caddy.Networks["egress-203-0-113-7"].InterfaceName != "egress1" {
		t.Fatalf("caddy networks = %+v", caddy.Networks)
	}
	agent := cfg.Services["agent"]
	if agent.Environment["SYLPH_EGRESS"] != "203.0.113.7=egress1" || agent.Environment["SYLPH_CORE_URL"] == "" || agent.NetworkMode != "service:caddy" {
		t.Fatalf("agent = %+v", agent)
	}
	if cfg.Services["xray"].NetworkMode != "service:caddy" {
		t.Fatalf("xray left the caddy namespace: %+v", cfg.Services["xray"])
	}
	if cfg.Networks["egress-203-0-113-7"].DriverOpts["com.docker.network.host_ipv4"] != "203.0.113.7" {
		t.Fatalf("networks = %+v", cfg.Networks)
	}
}

func TestIPAddAndRemove(t *testing.T) {
	fakeHost(t, "203.0.113.2", "203.0.113.7/32")
	probeEgress = func(ctx context.Context, ip string) (string, error) { return ip, nil }
	var calls []string
	old := compose.Exec
	t.Cleanup(func() { compose.Exec = old })
	started := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	compose.Exec = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		cmd := strings.Join(args, " ")
		calls = append(calls, cmd)
		switch {
		case cmd == "compose config --format json":
			return []byte(`{"services":{"caddy":{"restart":"unless-stopped"}}}`), nil
		case cmd == "compose ps -a --format json":
			return []byte(`{"ID":"a","Service":"caddy","State":"running"}`), nil
		case strings.HasPrefix(cmd, "inspect"):
			return []byte("a|" + started + "|0|0"), nil
		}
		return nil, nil
	}
	dir := t.TempDir()
	r, env := nodeEnv(t, "")
	if _, err := writeDeployFiles(dir, r); err != nil {
		t.Fatal(err)
	}
	if err := writeEnv(dir, r, env); err != nil {
		t.Fatal(err)
	}
	load := func() installed {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, ".env"))
		if err != nil {
			t.Fatal(err)
		}
		e, err := envfile.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		return installed{Role: r, Dir: dir, Env: e}
	}
	u := &ui.UI{Out: &bytes.Buffer{}}
	ctx := context.Background()

	if err := ipAdd(ctx, u, load(), nil, "203.0.113.7", true, false); err != nil {
		t.Fatal(err)
	}
	in := load()
	if in.Env.Get(keyEgressIPs) != "203.0.113.7" {
		t.Fatalf(".env %s = %q", keyEgressIPs, in.Env.Get(keyEgressIPs))
	}
	if data, err := os.ReadFile(filepath.Join(dir, overrideFile)); err != nil || !strings.Contains(string(data), "SYLPH_EGRESS: 203.0.113.7=egress1") {
		t.Fatalf("override = %s %v", data, err)
	}
	if !slices.Contains(calls, "compose up -d --remove-orphans") {
		t.Fatalf("role not brought up: %v", calls)
	}

	if err := ipAdd(ctx, u, in, []string{"203.0.113.7"}, "203.0.113.7", true, false); err != nil {
		t.Fatalf("adding twice: %v", err)
	}
	if err := ipAdd(ctx, u, in, []string{"203.0.113.7"}, "203.0.113.9", true, false); err == nil {
		t.Fatal("address that is not on the host accepted")
	}
	if err := ipRemove(ctx, u, in, []string{"203.0.113.7"}, "203.0.113.8", true); err == nil {
		t.Fatal("removing an unknown address accepted")
	}

	calls = nil
	if err := ipRemove(ctx, u, in, []string{"203.0.113.7"}, "203.0.113.7", true); err != nil {
		t.Fatal(err)
	}
	if got := load().Env.Get(keyEgressIPs); got != "" {
		t.Fatalf(".env after remove = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, overrideFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("override left after the last address: %v", err)
	}
	if !slices.Contains(calls, "compose up -d --remove-orphans") || !slices.Contains(calls, "network rm sylph-node_egress-203-0-113-7") {
		t.Fatalf("remove calls = %v", calls)
	}
}
