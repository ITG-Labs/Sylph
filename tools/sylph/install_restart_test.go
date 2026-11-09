package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
)

func stubReaders(t *testing.T, services map[string]time.Time) {
	t.Helper()
	old := compose.Exec
	t.Cleanup(func() { compose.Exec = old })
	compose.Exec = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		cmd := strings.Join(args, " ")
		switch {
		case cmd == "compose ps -a --format json":
			var rows []string
			for name := range services {
				rows = append(rows, `{"ID":"id-`+name+`","Service":"`+name+`","State":"running"}`)
			}
			return []byte(strings.Join(rows, "\n")), nil
		case strings.HasPrefix(cmd, "inspect"):
			var rows []string
			for name, started := range services {
				rows = append(rows, "id-"+name+"|"+started.UTC().Format(time.RFC3339Nano)+"|0|0")
			}
			return []byte(strings.Join(rows, "\n")), nil
		}
		return nil, nil
	}
}

func panelDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "certs"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"certs/cert.pem", "admins.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestStaleReadersFindsPanelStartedBeforeAdmins(t *testing.T) {
	panel, _ := role.Get("panel")
	dir := panelDir(t)
	now := time.Now()
	touch(t, filepath.Join(dir, "certs", "cert.pem"), now.Add(-3*time.Hour))
	touch(t, filepath.Join(dir, "admins.json"), now.Add(-time.Minute))
	stubReaders(t, map[string]time.Time{"panel": now.Add(-time.Hour)})
	if got := staleReaders(context.Background(), dir, panel); len(got) != 1 || got[0] != "panel" {
		t.Fatalf("stale = %v: a panel started before the last admins.json write holds the old admin map", got)
	}
	stubReaders(t, map[string]time.Time{"panel": now})
	if got := staleReaders(context.Background(), dir, panel); len(got) != 0 {
		t.Fatalf("stale = %v: a panel started after the files needs no restart", got)
	}
}

func TestStaleReadersFindsReaderStartedBeforeCerts(t *testing.T) {
	panel, _ := role.Get("panel")
	dir := panelDir(t)
	now := time.Now()
	touch(t, filepath.Join(dir, "admins.json"), now.Add(-3*time.Hour))
	touch(t, filepath.Join(dir, "certs", "cert.pem"), now.Add(-time.Minute))
	stubReaders(t, map[string]time.Time{"panel": now.Add(-time.Hour)})
	if got := staleReaders(context.Background(), dir, panel); len(got) != 1 {
		t.Fatalf("stale = %v", got)
	}
}

func TestStaleReadersIgnoresAdminsOfRolesWithoutThem(t *testing.T) {
	bot, _ := role.Get("bot")
	dir := panelDir(t)
	now := time.Now()
	touch(t, filepath.Join(dir, "certs", "cert.pem"), now.Add(-3*time.Hour))
	touch(t, filepath.Join(dir, "admins.json"), now.Add(-time.Minute))
	stubReaders(t, map[string]time.Time{"bot": now.Add(-time.Hour)})
	if got := staleReaders(context.Background(), dir, bot); len(got) != 0 {
		t.Fatalf("stale = %v", got)
	}
}

func TestWriteAdminsLeavesTheFileReadableByThePanel(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chown to the panel user needs root")
	}
	dir := t.TempDir()
	if err := writeAdmins(dir, []adminRecord{[]byte(`{"name":"a"}`)}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "admins.json"))
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if st.Uid != 65532 || st.Gid != 65532 || info.Mode().Perm() != 0o400 {
		t.Fatalf("admins.json is %d:%d %v, a restart without the perms step would leave the panel without access", st.Uid, st.Gid, info.Mode().Perm())
	}
	if err := writeAdmins(dir, []adminRecord{[]byte(`{"name":"b"}`)}); err != nil {
		t.Fatalf("rewrite over a 0400 file: %v", err)
	}
}
