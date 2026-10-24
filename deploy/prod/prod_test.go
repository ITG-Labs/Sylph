package prod

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"regexp"
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
