package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fakeProcDir(t *testing.T, procs map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for pid, cmdline := range procs {
		dir := filepath.Join(root, pid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.ReplaceAll(cmdline, " ", "\x00")+"\x00"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFindXrayAmongNodeProcesses(t *testing.T) {
	root := fakeProcDir(t, map[string]string{
		"1":    "caddy run --config /etc/caddy/caddy.json",
		"7":    "/usr/local/bin/xray run -c /etc/xray/config.json",
		"9":    "xray api statssys --server=127.0.0.1:10085",
		"11":   "/sylph/sylph-agent",
		"self": "/usr/local/bin/xray run -c /etc/xray/config.json",
		"20":   "",
	})
	pid, err := procXray{root: root}.Find()
	if err != nil || pid != 7 {
		t.Fatalf("pid = %d, err = %v", pid, err)
	}
}

func TestFindXrayWithImageDefaultCommand(t *testing.T) {
	root := fakeProcDir(t, map[string]string{"5": "/usr/local/bin/xray -confdir /usr/local/etc/xray/"})
	if pid, err := (procXray{root: root}).Find(); err != nil || pid != 5 {
		t.Fatalf("pid = %d, err = %v", pid, err)
	}
}

func TestFindXrayNoneOrSeveral(t *testing.T) {
	none := fakeProcDir(t, map[string]string{"1": "caddy run"})
	if _, err := (procXray{root: none}).Find(); !errors.Is(err, errXrayNotFound) {
		t.Fatalf("err = %v, want errXrayNotFound", err)
	}
	two := fakeProcDir(t, map[string]string{"3": "xray run -c a.json", "4": "xray run -c b.json"})
	if _, err := (procXray{root: two}).Find(); err == nil || !strings.Contains(err.Error(), "2 xray processes") {
		t.Fatalf("err = %v, want refusal to guess", err)
	}
}

func TestTerminateSendsSIGTERM(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := (procXray{}).Terminate(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
			t.Fatalf("process ended with %v, want SIGTERM", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("process did not stop")
	}
}
