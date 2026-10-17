package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/castore"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/bundle"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
)

func TestImageVersion(t *testing.T) {
	old := version.Version
	defer func() { version.Version = old }()
	cases := map[string]string{"0.2.0": "0.2.0", "edge-abc1234": "sha-abc1234"}
	for v, want := range cases {
		version.Version = v
		got, err := imageVersion("")
		if err != nil || got != want {
			t.Fatalf("imageVersion for %q = %q, %v", v, got, err)
		}
	}
	version.Version = "dev"
	if _, err := imageVersion(""); err == nil {
		t.Fatal("dev build must ask for --version")
	}
	if got, err := imageVersion("test-1"); err != nil || got != "test-1" {
		t.Fatalf("override = %q, %v", got, err)
	}
	if _, err := imageVersion("bad tag"); err == nil {
		t.Fatal("bad override should fail")
	}
}

func TestOlder(t *testing.T) {
	if !older("0.1.0", "0.2.0") || older("0.2.0", "0.1.0") || older("0.1.0", "0.1.0") || older("sha-abc", "0.1.0") {
		t.Fatal("older is wrong")
	}
	if !older("0.9.9", "0.10.0") {
		t.Fatal("older must compare numerically")
	}
}

func TestParseWithArgs(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "")
	dir := fs.String("dir", "", "")
	pos, err := parseWithArgs(fs, []string{"--dir", "/d", "panel", "--yes"})
	if err != nil || len(pos) != 1 || pos[0] != "panel" || !*yes || *dir != "/d" {
		t.Fatalf("pos = %v, yes = %v, dir = %q, err = %v", pos, *yes, *dir, err)
	}
}

func TestWriteDeployFilesKeepsBackup(t *testing.T) {
	dir := t.TempDir()
	r, _ := role.Get("panel")
	backups, err := writeDeployFiles(dir, r)
	if err != nil || len(backups) != 0 {
		t.Fatalf("first write: %v, %v", backups, err)
	}
	if backups, err := writeDeployFiles(dir, r); err != nil || len(backups) != 0 {
		t.Fatalf("same files must not leave backups: %v, %v", backups, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Caddyfile"), []byte("hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups, err = writeDeployFiles(dir, r)
	if err != nil || len(backups) != 1 || !strings.HasPrefix(backups[0], "Caddyfile.bak-") {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, backups[0])); string(data) != "hand edit\n" {
		t.Fatalf("backup = %q", data)
	}
	got, _ := roleOfDir(dir)
	if got == nil || got.Name != "panel" {
		t.Fatalf("roleOfDir = %v", got)
	}
}

func newBundle(t *testing.T, r string, name string) *bundle.Bundle {
	t.Helper()
	ca := t.TempDir()
	now := time.Now()
	if err := castore.Init(ca, "CA", now); err != nil {
		t.Fatal(err)
	}
	if r == "core" {
		castore.Issue(ca, role.DefaultCoreName, mtls.ServerUsage, "", time.Hour, now)
		castore.Issue(ca, "cron", mtls.ClientUsage, mtls.RoleCron, time.Hour, now)
	} else if err := castore.Issue(ca, name, mtls.ClientUsage, r, time.Hour, now); err != nil {
		t.Fatal(err)
	}
	b, err := bundle.Collect(ca, r, name, role.DefaultCoreName, now)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWriteCertsDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	b := newBundle(t, "core", "core")
	changed, err := writeCerts(dir, b)
	if err != nil || !changed {
		t.Fatalf("first write = %v, %v", changed, err)
	}
	for _, p := range []string{"ca.pem", "core/cert.pem", "core/key.pem", "cron/cert.pem", "cron/key.pem"} {
		st, err := os.Stat(filepath.Join(dir, "certs", p))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", p, st, err)
		}
	}
	if changed, err := writeCerts(dir, b); err != nil || changed {
		t.Fatalf("same bundle = %v, %v", changed, err)
	}
	r, _ := role.Get("core")
	leaves, err := currentCerts(dir, r, role.DefaultCoreName, time.Now())
	if err != nil || len(leaves) != 2 {
		t.Fatalf("currentCerts = %v, %v", leaves, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "certs", "revoked.json"), []byte(`{"serials":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCerts(dir, newBundle(t, "core", "core")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "certs", "revoked.json")); err != nil {
		t.Fatal("a bundle without revoked.json must not drop the one on the machine")
	}
}

func TestAdminsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	recs, names, err := readAdmins(dir)
	if err != nil || recs != nil || names != nil {
		t.Fatalf("missing file = %v %v %v", recs, names, err)
	}
	recs = append(recs, adminRecord(`{"name":"owner","password_hash":"h","totp_secret":"s"}`))
	if err := writeAdmins(dir, recs); err != nil {
		t.Fatal(err)
	}
	recs, names, err = readAdmins(dir)
	if err != nil || len(recs) != 1 || names[0] != "owner" {
		t.Fatalf("read back = %v %v %v", recs, names, err)
	}
	var decoded []map[string]string
	data, _ := os.ReadFile(filepath.Join(dir, "admins.json"))
	if err := json.Unmarshal(data, &decoded); err != nil || decoded[0]["password_hash"] != "h" {
		t.Fatalf("admins.json = %s", data)
	}
}

func TestPlanLoadCore(t *testing.T) {
	dir := t.TempDir()
	r, _ := role.Get("core")
	p := &plan{r: r, dir: dir}
	if err := p.load("0.2.0"); err != nil {
		t.Fatal(err)
	}
	pw := p.env.Get(role.KeyPassword)
	if len(pw) != 64 || role.ValidPassword(pw) != nil || !p.firstInstall {
		t.Fatalf("password = %q, first = %v", pw, p.firstInstall)
	}
	if _, err := writeDeployFiles(dir, r); err != nil {
		t.Fatal(err)
	}
	if err := writeEnv(dir, r, p.env); err != nil {
		t.Fatal(err)
	}
	again := &plan{r: r, dir: dir}
	if err := again.load("0.3.0"); err != nil {
		t.Fatal(err)
	}
	if again.env.Get(role.KeyPassword) != pw || again.prevVersion != "0.2.0" || again.firstInstall {
		t.Fatalf("rerun lost state: pw=%q prev=%q", again.env.Get(role.KeyPassword), again.prevVersion)
	}
	envFile := filepath.Join(t.TempDir(), "core.env")
	os.WriteFile(envFile, []byte("POSTGRES_PASSWORD=abcdefabcdefabcdefabcdef\n"), 0o600)
	if err := again.fromFile(installOpts{envFile: envFile}, "0.3.0"); err == nil || !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
		t.Fatalf("password change must be refused: %v", err)
	}

	lost := t.TempDir()
	os.MkdirAll(filepath.Join(lost, "data", "postgres"), 0o700)
	if err := (&plan{r: r, dir: lost}).load("0.2.0"); err == nil {
		t.Fatal("database without .env must be refused")
	}
}

func TestPlanLoadRefusesOtherRoleDir(t *testing.T) {
	dir := t.TempDir()
	panel, _ := role.Get("panel")
	bot, _ := role.Get("bot")
	writeDeployFiles(dir, panel)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SYLPH_VERSION=0.1.0\n"), 0o600)
	if err := (&plan{r: bot, dir: dir}).load("0.1.0"); err == nil || !strings.Contains(err.Error(), "стоит роль panel") {
		t.Fatalf("err = %v", err)
	}
}

func TestFindRolesInWorkingDir(t *testing.T) {
	old := compose.Exec
	compose.Exec = func(ctx context.Context, dir string, args ...string) ([]byte, error) { return []byte("[]"), nil }
	defer func() { compose.Exec = old }()
	base := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	os.Chdir(base)
	for _, name := range []string{"node", "core"} {
		r, _ := role.Get(name)
		dir := filepath.Join(base, "sylph-"+name)
		os.MkdirAll(dir, 0o755)
		writeDeployFiles(dir, r)
		os.WriteFile(filepath.Join(dir, ".env"), []byte("SYLPH_VERSION=0.1.0\n"), 0o600)
	}
	os.MkdirAll(filepath.Join(base, "sylph-junk"), 0o755)
	found, err := findRoles(context.Background(), "")
	if err != nil || len(found) != 2 || found[0].Role.Name != "core" || found[1].Role.Name != "node" {
		t.Fatalf("found = %+v, %v", found, err)
	}
}

func TestCheckRoleDir(t *testing.T) {
	base, err := os.MkdirTemp(".", ".roledir-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	base, _ = filepath.Abs(base)
	if err := checkRoleDir(filepath.Join(base, "sylph-panel")); err != nil {
		t.Fatalf("new dir: %v", err)
	}
	busy := filepath.Join(base, "srv")
	os.MkdirAll(busy, 0o755)
	os.WriteFile(filepath.Join(busy, "other.txt"), []byte("x"), 0o644)
	if err := checkRoleDir(busy); err == nil || !strings.Contains(err.Error(), "не пустая") {
		t.Fatalf("non-empty foreign dir: %v", err)
	}
	lab := filepath.Join(base, "sylph-panel")
	os.MkdirAll(lab, 0o755)
	os.WriteFile(filepath.Join(lab, "compose.override.yml"), []byte("x"), 0o644)
	if err := checkRoleDir(lab); err != nil {
		t.Fatalf("sylph-* dir with an override: %v", err)
	}
	for _, d := range []string{"/tmp/sylph-core", "/var/tmp/x", "/dev/shm/sylph-node"} {
		if err := checkRoleDir(d); err == nil {
			t.Fatalf("%s must be refused", d)
		}
	}
}

func fakeDocker(t *testing.T, responses map[string]string) {
	t.Helper()
	old := compose.Exec
	compose.Exec = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return []byte(responses[strings.Join(args, " ")]), nil
	}
	t.Cleanup(func() { compose.Exec = old })
}

func TestNotInstalledElsewhere(t *testing.T) {
	fakeDocker(t, map[string]string{"compose ls -a --format json": `[{"Name":"sylph-core","Status":"running(3)","ConfigFiles":"/root/sylph-core/compose.yml"}]`})
	core, _ := role.Get("core")
	panel, _ := role.Get("panel")
	if err := notInstalledElsewhere(context.Background(), core, "/home/ops/sylph-core"); err == nil || !strings.Contains(err.Error(), "/root/sylph-core") {
		t.Fatalf("second core: %v", err)
	}
	if err := notInstalledElsewhere(context.Background(), core, "/root/sylph-core"); err != nil {
		t.Fatalf("same dir: %v", err)
	}
	if err := notInstalledElsewhere(context.Background(), panel, "/home/ops/sylph-panel"); err != nil {
		t.Fatalf("other role: %v", err)
	}
}

func TestRunningAt(t *testing.T) {
	fakeDocker(t, map[string]string{
		"compose ps -a --format json": `{"ID":"a","Service":"core","State":"running","Image":"ghcr.io/itg-labs/sylph-core:0.1.0"}
{"ID":"b","Service":"postgres","State":"running","Image":"postgres:17-alpine"}`,
	})
	if !runningAt(context.Background(), "/x", "0.1.0") || runningAt(context.Background(), "/x", "0.2.0") {
		t.Fatal("runningAt must compare sylph image tags")
	}
}

func TestPlanKeepsPasswordFromEmptyEnvValue(t *testing.T) {
	dir := t.TempDir()
	r, _ := role.Get("core")
	p := &plan{r: r, dir: dir}
	if err := p.load("0.1.0"); err != nil {
		t.Fatal(err)
	}
	writeDeployFiles(dir, r)
	writeEnv(dir, r, p.env)
	pw := p.env.Get(role.KeyPassword)
	again := &plan{r: r, dir: dir}
	if err := again.load("0.1.0"); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(t.TempDir(), "core.env")
	os.WriteFile(envFile, []byte("POSTGRES_PASSWORD=\nSYLPH_STATS_TZ=UTC\n"), 0o600)
	if err := again.fromFile(installOpts{envFile: envFile}, "0.1.0"); err != nil {
		t.Fatalf("empty password in --env must mean keep: %v", err)
	}
	if again.env.Get(role.KeyPassword) != pw || again.env.Get("SYLPH_STATS_TZ") != "UTC" {
		t.Fatalf("password %q, tz %q", again.env.Get(role.KeyPassword), again.env.Get("SYLPH_STATS_TZ"))
	}
	os.MkdirAll(filepath.Join(dir, "data", "postgres"), 0o700)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SYLPH_VERSION=0.1.0\nPOSTGRES_PASSWORD=\n"), 0o600)
	if err := (&plan{r: r, dir: dir}).load("0.1.0"); err == nil {
		t.Fatal("a database with an empty password in .env must be refused")
	}
}
