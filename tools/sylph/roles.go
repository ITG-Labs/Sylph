package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/tools/sylph/internal/bundle"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/envfile"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
)

type installed struct {
	Role *role.Role
	Dir  string
	Env  *envfile.Env
}

func roleDir(dir string, r *role.Role) (string, error) {
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(cwd, "sylph-"+r.Name)
	}
	return filepath.Abs(dir)
}

func roleOfDir(dir string) (*role.Role, error) {
	f, err := os.Open(filepath.Join(dir, "compose.yml"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if name, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "name: sylph-"); ok {
			return role.Get(strings.TrimSpace(name))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%s: это не папка роли sylph", dir)
}

func readEnv(dir string) (*envfile.Env, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		return nil, err
	}
	env, err := envfile.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s/.env: %w", dir, err)
	}
	return env, nil
}

func findRoles(ctx context.Context, dir string) ([]installed, error) {
	var dirs []string
	if dir != "" {
		dirs = append(dirs, dir)
	} else {
		if projects, err := compose.Ls(ctx); err == nil {
			for _, p := range projects {
				if strings.HasPrefix(p.Name, "sylph-") {
					dirs = append(dirs, p.Dir)
				}
			}
		}
		if matches, err := filepath.Glob("sylph-*"); err == nil {
			dirs = append(dirs, matches...)
		}
	}
	seen := map[string]bool{}
	var found []installed
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		r, err := roleOfDir(abs)
		if err != nil {
			if dir != "" {
				return nil, err
			}
			continue
		}
		env, err := readEnv(abs)
		if err != nil {
			if dir != "" {
				return nil, err
			}
			continue
		}
		found = append(found, installed{Role: r, Dir: abs, Env: env})
	}
	order := map[string]int{}
	for i, n := range []string{"core", "node", "panel", "bot", "subs", "monitor"} {
		order[n] = i
	}
	sort.SliceStable(found, func(i, j int) bool { return order[found[i].Role.Name] < order[found[j].Role.Name] })
	return found, nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	err = tmp.Chmod(mode)
	if err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

func sameFile(path string, data []byte) bool {
	cur, err := os.ReadFile(path)
	return err == nil && bytes.Equal(cur, data)
}

func writeDeployFiles(dir string, r *role.Role) ([]string, error) {
	var backups []string
	for _, name := range r.DeployFiles() {
		data, err := r.File(name)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, name)
		if sameFile(path, data) {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			if err := os.Rename(path, path+".bak"); err != nil {
				return nil, err
			}
			backups = append(backups, name+".bak")
		}
		if err := writeFileAtomic(path, data, 0o644); err != nil {
			return nil, err
		}
	}
	return backups, nil
}

func writeEnv(dir string, r *role.Role, env *envfile.Env) error {
	_, tmpl, err := r.Template()
	if err != nil {
		return err
	}
	data, err := envfile.Render(tmpl, env)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, ".env"), data, 0o600)
}

func writeCerts(dir string, b *bundle.Bundle) (bool, error) {
	changed := false
	certs := filepath.Join(dir, "certs")
	for _, path := range bundle.Paths(b.Manifest.Role) {
		data, ok := b.Files[path]
		if !ok {
			continue
		}
		full := filepath.Join(certs, filepath.FromSlash(path))
		if sameFile(full, data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return changed, err
		}
		if err := writeFileAtomic(full, data, 0o600); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

func currentCerts(dir string, r *role.Role, coreName string, now time.Time) ([]bundle.Leaf, error) {
	b := &bundle.Bundle{Manifest: bundle.Manifest{Role: r.Name}, Files: map[string][]byte{}}
	for _, path := range bundle.Paths(r.Name) {
		data, err := os.ReadFile(filepath.Join(dir, "certs", filepath.FromSlash(path)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		b.Files[path] = data
	}
	if len(b.Files) == 0 {
		return nil, fs.ErrNotExist
	}
	return b.Verify(r.Name, coreName, now)
}

type adminRecord = json.RawMessage

func readAdmins(dir string) ([]adminRecord, []string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "admins.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var records []adminRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, nil, fmt.Errorf("admins.json: %w", err)
	}
	var names []string
	for _, rec := range records {
		var n struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(rec, &n); err != nil {
			return nil, nil, fmt.Errorf("admins.json: %w", err)
		}
		names = append(names, n.Name)
	}
	return records, names, nil
}

func writeAdmins(dir string, records []adminRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "admins.json"), append(data, '\n'), 0o600)
}

func chownToSudoUser(path string) error {
	uid, err1 := strconv.Atoi(os.Getenv("SUDO_UID"))
	gid, err2 := strconv.Atoi(os.Getenv("SUDO_GID"))
	if err1 != nil || err2 != nil || uid == 0 {
		return nil
	}
	return os.Lchown(path, uid, gid)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func projectRunning(ctx context.Context, dir string) bool {
	containers, err := compose.Snapshot(ctx, dir)
	if err != nil {
		return false
	}
	for _, c := range containers {
		if c.State == "running" {
			return true
		}
	}
	return false
}
