package prod

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestFilesCarryEveryRole(t *testing.T) {
	for _, role := range []string{"core", "panel", "bot", "subs", "node", "monitor"} {
		for _, name := range []string{"compose.yml", ".env.example"} {
			if _, err := fs.Stat(Files, role+"/"+name); err != nil {
				t.Fatalf("%s/%s: %v", role, name, err)
			}
		}
	}
	for _, role := range []string{"panel", "bot", "subs"} {
		if _, err := fs.Stat(Files, role+"/Caddyfile"); err != nil {
			t.Fatalf("%s/Caddyfile: %v", role, err)
		}
	}
}

var caddyBuilds = map[string]string{
	"2.11.7-l4.0.1.2": "7c7d67a5a51c660c35e34a327f42f4018ff4b9ab032e20120c0d15fef2875b0e",
	"2.11.8-l4.0.1.2": "b194484ff91e4a6d972f56545419bcb74350434ca709e4b8c80683f8c15d56cf",
}

func TestCaddyTagFollowsItsBuild(t *testing.T) {
	env, err := fs.ReadFile(Files, "node/.env.example")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^CADDY_VERSION=(\S+)$`).FindSubmatch(env)
	if m == nil {
		t.Fatal("node/.env.example: no CADDY_VERSION")
	}
	tag := string(m[1])
	dockerfile, err := os.ReadFile("../caddy/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	conf, err := os.ReadFile("../caddy/caddy.json")
	if err != nil {
		t.Fatal(err)
	}
	find := func(re string) string {
		m := regexp.MustCompile(re).FindSubmatch(dockerfile)
		if m == nil {
			t.Fatalf("deploy/caddy/Dockerfile: no match for %s", re)
		}
		return string(m[1])
	}
	builder, xcaddy, runtime, l4 := find(`FROM caddy:(\S+)-builder`), find(`xcaddy build v(\S+)`), find(`(?m)^FROM caddy:([0-9.]+)\s*$`), find(`caddy-l4@v(\S+)`)
	if builder != xcaddy || builder != runtime {
		t.Fatalf("deploy/caddy/Dockerfile: caddy versions differ: builder %s, xcaddy %s, runtime %s", builder, xcaddy, runtime)
	}
	prefix := builder + "-l4." + l4
	if tag != prefix && !regexp.MustCompile(`^`+regexp.QuoteMeta(prefix)+`-r([2-9]|[1-9][0-9]+)$`).MatchString(tag) {
		t.Fatalf("CADDY_VERSION=%s does not match the build: want %s (or %s-r2, -r3… when only the build changes)", tag, prefix, prefix)
	}
	sum := sha256.Sum256(append(append(dockerfile, 0), conf...))
	got := hex.EncodeToString(sum[:])
	recorded, known := caddyBuilds[tag]
	switch {
	case !known:
		t.Fatalf("new Caddy tag %s: record caddyBuilds[%q] = %q", tag, tag, got)
	case recorded != got:
		t.Fatalf("deploy/caddy changed but CADDY_VERSION is still %s, which is already published and is never re-pushed: give the build a new tag (%s-r2, -r3… or new versions) in node/.env.example and record its hash, keep the old entry as it is", tag, prefix)
	}
	compose, err := fs.ReadFile(Files, "node/compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "sylph-caddy:${CADDY_VERSION:?") {
		t.Fatal("node/compose.yml: caddy image must use CADDY_VERSION")
	}
}

var xrayBuilds = map[string]string{
	"26.11.9": "dd92859649612c0168ca3a54d4d1267eada101f27dc6dfe7cfe8e31e3050c6c5",
}

func TestXrayTagFollowsItsBuild(t *testing.T) {
	env, err := fs.ReadFile(Files, "node/.env.example")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^XRAY_VERSION=(\S+)$`).FindSubmatch(env)
	if m == nil {
		t.Fatal("node/.env.example: no XRAY_VERSION")
	}
	tag := string(m[1])
	dockerfile, err := os.ReadFile("../xray/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	arg := func(name, pattern string) string {
		m := regexp.MustCompile(`(?m)^ARG ` + name + `=(\S+)$`).FindSubmatch(dockerfile)
		if m == nil || !regexp.MustCompile(`^`+pattern+`$`).Match(m[1]) {
			t.Fatalf("deploy/xray/Dockerfile: ARG %s must have a default matching %s", name, pattern)
		}
		return string(m[1])
	}
	if v := arg("XRAY_VERSION", `[0-9]+\.[0-9]+\.[0-9]+`); v != tag {
		t.Fatalf("deploy/xray/Dockerfile builds xray %s, node/.env.example pins XRAY_VERSION=%s", v, tag)
	}
	arg("XRAY_COMMIT", `[0-9a-f]{40}`)
	arg("RULES_DAT", `[0-9]{14}`)
	arg("GEOIP_SHA256", `[0-9a-f]{64}`)
	arg("GEOSITE_SHA256", `[0-9a-f]{64}`)
	for _, re := range []string{`(?m)^FROM golang:[0-9]+\.[0-9]+\.[0-9]+-alpine AS build$`, `rev-parse HEAD`, `sha256sum -c`, `(?m)^FROM gcr\.io/distroless/static-debian12:nonroot$`, `(?m)^USER 65532:65532$`} {
		if !regexp.MustCompile(re).Match(dockerfile) {
			t.Fatalf("deploy/xray/Dockerfile: no match for %s", re)
		}
	}
	if strings.Contains(string(dockerfile), "latest") {
		t.Fatal("deploy/xray/Dockerfile must not fetch anything by latest: the build has to be reproducible")
	}
	sum := sha256.Sum256(dockerfile)
	got := hex.EncodeToString(sum[:])
	recorded, known := xrayBuilds[tag]
	switch {
	case !known:
		t.Fatalf("new Xray tag %s: record xrayBuilds[%q] = %q", tag, tag, got)
	case recorded != got:
		t.Fatalf("deploy/xray changed but XRAY_VERSION is still %s, which is already published and is never re-pushed: ship the change with a new Xray version and record its hash, keep the old entry as it is", tag)
	}
	compose, err := fs.ReadFile(Files, "node/compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(serviceBlock(t, string(compose), "xray"), "image: ghcr.io/itg-labs/sylph-xray:${XRAY_VERSION:?") {
		t.Fatal("node/compose.yml: xray image must be ghcr.io/itg-labs/sylph-xray:${XRAY_VERSION}")
	}
	for _, name := range []string{"../test/xray/docker-compose.yml", "../test/node/docker-compose.yml", "../test/node/agent.Dockerfile", "../../Makefile", "node/compose.yml"} {
		path := name
		if !strings.HasPrefix(name, "../") {
			path = "./" + name
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "xtls/xray-core") {
			t.Fatalf("%s: stands and nodes run the own image ghcr.io/itg-labs/sylph-xray, not ghcr.io/xtls/xray-core", name)
		}
		for _, m := range regexp.MustCompile(`sylph-xray:(\S+)`).FindAllSubmatch(data, -1) {
			if v := string(m[1]); v != tag && !strings.HasPrefix(v, "$") {
				t.Fatalf("%s: sylph-xray:%s, want the pinned %s", name, v, tag)
			}
		}
	}
}

func serviceBlock(t *testing.T, compose, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `:\n((?:    .*\n|\n)*)`).FindStringSubmatch(compose)
	if m == nil {
		t.Fatalf("no service %s", name)
	}
	return m[1]
}

func TestNodeXrayRunsWithoutKernelSplice(t *testing.T) {
	prod, err := fs.ReadFile(Files, "node/compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	stand, err := os.ReadFile("../test/node/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	want := regexp.MustCompile(`(?m)^\s+XRAY_BUF_SPLICE: ["']?disable["']?\s*$`)
	for name, compose := range map[string]string{"prod/node/compose.yml": string(prod), "test/node/docker-compose.yml": string(stand)} {
		if !want.MatchString(serviceBlock(t, compose, "xray")) {
			t.Fatalf("%s: service xray must set XRAY_BUF_SPLICE: disable, otherwise Vision downlink is spliced and its counter grows only when the connection closes", name)
		}
	}
	subs, err := os.ReadFile("../test/subs/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"xray", "xray-2"} {
		block := serviceBlock(t, string(subs), name)
		if !strings.Contains(block, "service: xray") || regexp.MustCompile(`(?m)^    environment:`).MatchString(block) && !want.MatchString(block) {
			t.Fatalf("test/subs/docker-compose.yml: service %s must extend prod xray and keep its environment", name)
		}
	}
}

func TestAgentImageCapsAreGrantedToEveryServiceOfIt(t *testing.T) {
	dockerfile, err := os.ReadFile("../agent/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`setcap (\S+)\+ep`).FindSubmatch(dockerfile)
	if m == nil {
		t.Fatal("deploy/agent/Dockerfile: no setcap")
	}
	caps := strings.Split(strings.ToUpper(strings.ReplaceAll(string(m[1]), "cap_", "")), ",")
	if !slices.Contains(caps, "NET_ADMIN") {
		t.Fatalf("agent binary caps = %v: destroying sockets of a removed user needs NET_ADMIN", caps)
	}
	compose, err := fs.ReadFile(Files, "node/compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bootstrap", "agent"} {
		block := serviceBlock(t, string(compose), name)
		granted := regexp.MustCompile(`cap_add: \[([^\]]*)\]`).FindStringSubmatch(block)
		if granted == nil {
			t.Fatalf("service %s: no cap_add", name)
		}
		for _, c := range caps {
			if !slices.Contains(strings.Split(strings.ReplaceAll(granted[1], " ", ""), ","), c) {
				t.Fatalf("service %s: cap_add [%s] lacks %s: a binary with a file capability outside the bounding set does not start", name, granted[1], c)
			}
		}
		if !strings.Contains(block, "*locked") {
			t.Fatalf("service %s must stay on the locked profile with no-new-privileges", name)
		}
	}
}

func imageGodebug(t *testing.T, image string) (string, bool) {
	t.Helper()
	name, _, _ := strings.Cut(image, ":")
	dockerfiles := map[string]string{
		"ghcr.io/itg-labs/sylph-caddy":   "../caddy/Dockerfile",
		"ghcr.io/itg-labs/sylph-panel":   "../panel/Dockerfile",
		"ghcr.io/itg-labs/sylph-core":    "../service.Dockerfile",
		"ghcr.io/itg-labs/sylph-subs":    "../service.Dockerfile",
		"ghcr.io/itg-labs/sylph-bot":     "../service.Dockerfile",
		"ghcr.io/itg-labs/sylph-cron":    "../service.Dockerfile",
		"ghcr.io/itg-labs/sylph-monitor": "../service.Dockerfile",
	}
	if name == "caddy" {
		return "", true
	}
	path, ok := dockerfiles[name]
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stages := regexp.MustCompile(`(?m)^FROM `).Split(string(data), -1)
	if m := regexp.MustCompile(`GODEBUG=(\S+)`).FindStringSubmatch(stages[len(stages)-1]); m != nil {
		return strings.Trim(m[1], `"'\`), true
	}
	return "", true
}

func TestGoListenersRunWithoutMPTCP(t *testing.T) {
	public := map[string]bool{}
	for _, role := range []string{"core", "panel", "bot", "subs", "node", "monitor"} {
		compose, err := fs.ReadFile(Files, role+"/compose.yml")
		if err != nil {
			t.Fatal(err)
		}
		body := regexp.MustCompile(`(?ms)^services:\n(.*?)(?:^\S|\z)`).FindStringSubmatch(string(compose))
		if body == nil {
			t.Fatalf("%s/compose.yml: no services", role)
		}
		for _, m := range regexp.MustCompile(`(?m)^  ([a-z0-9-]+):$`).FindAllStringSubmatch(body[1], -1) {
			name := m[1]
			block := serviceBlock(t, string(compose), name)
			im := regexp.MustCompile(`(?m)^    image: (\S+)`).FindStringSubmatch(block)
			if im == nil {
				t.Fatalf("%s/%s: no image", role, name)
			}
			published := regexp.MustCompile(`(?m)^    ports:`).MatchString(block)
			godebug, golang := imageGodebug(t, im[1])
			if !golang {
				if published {
					t.Fatalf("%s/%s publishes a port from %s: tell this test whether it is a Go program and where its GODEBUG comes from", role, name, im[1])
				}
				continue
			}
			if env := regexp.MustCompile(`(?m)^      GODEBUG: ["']?([^"'\s]+)["']?$`).FindStringSubmatch(block); env != nil {
				godebug = env[1]
			}
			if !slices.Contains(strings.Split(godebug, ","), "multipathtcp=0") {
				t.Fatalf("%s/%s (%s): GODEBUG=%q lacks multipathtcp=0: Go 1.24+ accepts MPTCP on listening sockets by default", role, name, im[1], godebug)
			}
			if published {
				public[role+"/"+name] = true
			}
		}
	}
	for _, want := range []string{"core/core", "panel/caddy", "bot/caddy", "subs/caddy", "node/caddy"} {
		if !public[want] {
			t.Fatalf("%s: expected a published Go listener, got %v", want, public)
		}
	}
}
