package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/selfupdate"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
)

type fakeRelease struct {
	tag     string
	binary  []byte
	sums    string
	latestN int
}

func withRelease(t *testing.T, cur string, rel *fakeRelease) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /latest", func(w http.ResponseWriter, _ *http.Request) {
		rel.latestN++
		if rel.tag == "" {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		fmt.Fprintf(w, `{"tag_name":%q}`, rel.tag)
	})
	mux.HandleFunc("GET /dl/{tag}/{name}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("name") {
		case selfupdate.Asset:
			w.Write(rel.binary)
		case selfupdate.Sums:
			w.Write([]byte(rel.sums))
		}
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	exe := filepath.Join(dir, "sylph")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldVer, oldRel, oldExe := version.Version, releases, executable
	version.Version = cur
	releases = func() *selfupdate.Client {
		c := selfupdate.New(cur)
		c.HTTP, c.API, c.Download = srv.Client(), srv.URL+"/latest", srv.URL+"/dl"
		return c
	}
	executable = func() (string, error) { return exe, nil }
	t.Setenv(envSelfUpdated, "")
	os.Unsetenv(envSelfUpdated)
	t.Cleanup(func() { version.Version, releases, executable = oldVer, oldRel, oldExe })
	return exe
}

func script(prints string) []byte {
	return []byte("#!/bin/sh\n[ \"$1\" = version ] && echo '" + prints + "'\n")
}

func sums(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:]) + "  " + selfupdate.Asset + "\n"
}

func TestSelfUpdateSwapsAndAsksForRestart(t *testing.T) {
	bin := script("sylph 0.3.0 (образы: 0.3.0)")
	exe := withRelease(t, "0.2.0", &fakeRelease{tag: "v0.3.0", binary: bin, sums: sums(bin)})
	err := selfUpdate(context.Background(), &ui.UI{Out: &bytes.Buffer{}}, []string{"update", "--yes"})
	var restart *restartRequest
	if !errors.As(err, &restart) {
		t.Fatalf("err = %v, want a restart request", err)
	}
	if restart.exe != exe || restart.from != "0.2.0" || strings.Join(restart.args, " ") != "update --yes" {
		t.Fatalf("restart = %+v", restart)
	}
	if data, _ := os.ReadFile(exe); !bytes.Equal(data, bin) {
		t.Fatalf("binary not swapped: %q", data)
	}
}

func TestSelfUpdateKeepsGoingWithoutANewerRelease(t *testing.T) {
	cases := map[string]*fakeRelease{
		"same version":  {tag: "v0.2.0"},
		"older release": {tag: "v0.1.1"},
		"github down":   {},
	}
	for name, rel := range cases {
		t.Run(name, func(t *testing.T) {
			exe := withRelease(t, "0.2.0", rel)
			out := &bytes.Buffer{}
			if err := selfUpdate(context.Background(), &ui.UI{Out: out}, []string{"update"}); err != nil {
				t.Fatalf("err = %v", err)
			}
			if rel.latestN != 1 {
				t.Fatalf("asked GitHub %d times", rel.latestN)
			}
			if data, _ := os.ReadFile(exe); string(data) != "old" {
				t.Fatalf("binary touched: %q", data)
			}
			if name == "github down" && !strings.Contains(out.String(), "не смог проверить") {
				t.Fatalf("no warning: %s", out)
			}
		})
	}
}

func TestSelfUpdateStopsOnBadDownload(t *testing.T) {
	bin := script("sylph 0.3.0 (образы: 0.3.0)")
	exe := withRelease(t, "0.2.0", &fakeRelease{tag: "v0.3.0", binary: bin, sums: sums([]byte("other"))})
	err := selfUpdate(context.Background(), &ui.UI{Out: &bytes.Buffer{}}, []string{"update"})
	var restart *restartRequest
	if err == nil || errors.As(err, &restart) || !errors.Is(err, selfupdate.ErrChecksum) {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "old" {
		t.Fatalf("binary replaced: %q", data)
	}
}

func TestSelfUpdateSkips(t *testing.T) {
	rel := &fakeRelease{tag: "v9.0.0"}
	withRelease(t, "edge-abc1234", rel)
	if err := selfUpdate(context.Background(), &ui.UI{Out: &bytes.Buffer{}}, nil); err != nil || rel.latestN != 0 {
		t.Fatalf("edge build: err %v, asked %d", err, rel.latestN)
	}
	withRelease(t, "0.3.0", rel)
	t.Setenv(envSelfUpdated, "0.2.0")
	out := &bytes.Buffer{}
	if err := selfUpdate(context.Background(), &ui.UI{Out: out}, nil); err != nil || rel.latestN != 0 || !strings.Contains(out.String(), "0.2.0 → 0.3.0") {
		t.Fatalf("after restart: err %v, asked %d, out %s", err, rel.latestN, out)
	}
}

func TestRestartExecsNewBinaryOnce(t *testing.T) {
	old := execve
	t.Cleanup(func() { execve = old })
	t.Setenv(envSelfUpdated, "stale")
	var gotPath string
	var gotArgs, gotEnv []string
	execve = func(path string, args []string, env []string) error {
		gotPath, gotArgs, gotEnv = path, args, env
		return errors.New("exec refused")
	}
	r := &restartRequest{exe: "/root/sylph", args: []string{"update", "--dir", "/root/sylph-node"}, from: "0.2.0"}
	if code := r.run(&ui.UI{Out: &bytes.Buffer{}}); code != 1 {
		t.Fatalf("failed exec returned %d", code)
	}
	if gotPath != "/root/sylph" || strings.Join(gotArgs, " ") != "/root/sylph update --dir /root/sylph-node" {
		t.Fatalf("exec %s %v", gotPath, gotArgs)
	}
	n := 0
	for _, kv := range gotEnv {
		if strings.HasPrefix(kv, envSelfUpdated+"=") {
			n++
			if kv != envSelfUpdated+"=0.2.0" {
				t.Fatalf("marker %q", kv)
			}
		}
	}
	if n != 1 {
		t.Fatalf("marker set %d times", n)
	}
}

func TestStageRoleUsesStagedFilesAndOperatorOverride(t *testing.T) {
	dir := t.TempDir()
	r, env := nodeEnv(t, "203.0.113.7")
	if err := os.WriteFile(filepath.Join(dir, operatorOverride), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, ".sylph-stage-old")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	stage, files, err := stageRole(installed{Role: r, Dir: dir, Env: env}, env.Clone())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(stage) != dir {
		t.Fatalf("stage %s is not inside the role dir", stage)
	}
	if st, err := os.Stat(stage); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("stage mode %v, %v", st, err)
	}
	want := []string{filepath.Join(stage, "compose.yml"), filepath.Join(dir, operatorOverride), filepath.Join(stage, egressFile)}
	if strings.Join(files.Files, " ") != strings.Join(want, " ") || files.EnvFile != filepath.Join(stage, ".env") || files.ProjectDir != dir {
		t.Fatalf("files = %+v", files)
	}
	if exists(stale) {
		t.Fatal("stale stage from an interrupted update left behind")
	}
	if exists(filepath.Join(dir, egressFile)) || exists(filepath.Join(dir, ".env")) || exists(filepath.Join(dir, "compose.yml")) {
		t.Fatal("staging wrote into the role dir")
	}
	p := &rolePlan{stage: stage}
	p.cleanup()
	if exists(stage) {
		t.Fatal("stage left behind")
	}
}

func TestStagedConfigMatchesInstalled(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	for _, ips := range []string{"", "203.0.113.7"} {
		dir := t.TempDir()
		r, env := nodeEnv(t, ips)
		env.Set("SYLPH_CORE_URL", "https://203.0.113.10:443")
		if _, err := writeDeployFiles(dir, r); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, operatorOverride), []byte("services:\n  caddy:\n    mem_limit: 256m\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		installedEnv := env.Clone()
		if err := prepareEgress(dir, r, installedEnv); err != nil {
			t.Fatal(err)
		}
		if err := writeEnv(dir, r, installedEnv); err != nil {
			t.Fatal(err)
		}
		hash := func(args ...string) string {
			cmd := exec.Command("docker", append(append([]string{"compose"}, args...), "config", "--hash", "*")...)
			cmd.Dir = dir
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("compose config %v: %v", args, err)
			}
			return string(out)
		}
		stage, files, err := stageRole(installed{Role: r, Dir: dir, Env: installedEnv}, env.Clone())
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(stage)
		flags := []string{"--project-directory", files.ProjectDir, "--env-file", files.EnvFile}
		for _, f := range files.Files {
			flags = append(flags, "-f", f)
		}
		if got, want := hash(flags...), hash(); got != want {
			t.Fatalf("egress %q: staged config differs from installed\nstaged:\n%s\ninstalled:\n%s", ips, got, want)
		}
	}
}

func TestCutWarning(t *testing.T) {
	node, _ := role.Get("node")
	panel, _ := role.Get("panel")
	_, env := nodeEnv(t, "")
	newer := env.Clone()
	newer.Set(role.KeyCaddyVersion, "2.12.0-l4.0.2.0")
	cases := []struct {
		name string
		p    rolePlan
		cut  bool
	}{
		{"agent only", rolePlan{in: installed{Role: node, Env: env}, env: env, restart: []compose.Service{{Name: "agent"}}}, false},
		{"caddy", rolePlan{in: installed{Role: node, Env: env}, env: newer, restart: []compose.Service{{Name: "agent"}, {Name: "caddy"}, {Name: "xray"}}}, true},
		{"xray only", rolePlan{in: installed{Role: node, Env: env}, env: env, restart: []compose.Service{{Name: "xray"}}}, true},
		{"unknown, same caddy", rolePlan{in: installed{Role: node, Env: env}, env: env, planErr: errors.New("x")}, true},
		{"unknown, new caddy", rolePlan{in: installed{Role: node, Env: env}, env: newer, planErr: errors.New("x")}, true},
		{"panel caddy", rolePlan{in: installed{Role: panel, Env: env}, env: env, restart: []compose.Service{{Name: "caddy"}}}, false},
	}
	for _, c := range cases {
		if _, cut := c.p.cutWarning(); cut != c.cut {
			t.Fatalf("%s: cut = %v", c.name, cut)
		}
	}
}
