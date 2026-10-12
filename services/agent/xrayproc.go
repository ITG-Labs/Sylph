package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var errXrayNotFound = errors.New("no xray process in the shared PID namespace")

type xrayProcess interface {
	Find() (int, error)
	Terminate(pid int) error
}

type procXray struct {
	root string
}

func (p procXray) Find() (int, error) {
	entries, err := os.ReadDir(p.root)
	if err != nil {
		return 0, fmt.Errorf("list processes: %w", err)
	}
	var found []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(p.root, e.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		if isXrayServer(strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")) {
			found = append(found, pid)
		}
	}
	switch len(found) {
	case 0:
		return 0, errXrayNotFound
	case 1:
		return found[0], nil
	default:
		return 0, fmt.Errorf("%d xray processes in the shared PID namespace, want one", len(found))
	}
}

func isXrayServer(args []string) bool {
	if filepath.Base(args[0]) != "xray" {
		return false
	}
	return len(args) == 1 || args[1] == "run" || strings.HasPrefix(args[1], "-")
}

func (procXray) Terminate(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
