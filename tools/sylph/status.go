package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/machine"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
)

func cmdStatus(ctx context.Context, u *ui.UI, args []string) int {
	fs := newFlags(u, "status", "sudo ./sylph status [флаги]")
	dir := fs.String("dir", "", "показать только роль из этой папки")
	if _, err := parseWithArgs(fs, args); err != nil {
		return 2
	}
	if err := machine.Root(); err != nil {
		u.Fail(err.Error())
		return 1
	}
	roles, err := findRoles(ctx, *dir)
	if err != nil {
		u.Fail(err.Error())
		return 1
	}
	if len(roles) == 0 {
		u.Fail("ролей sylph на машине не нашёл (ищу в docker compose ls и ./sylph-*); укажи --dir")
		return 1
	}
	bad := false
	now := time.Now()
	for _, in := range roles {
		body, ok := statusCard(ctx, in, now)
		color := ui.Green
		if !ok {
			color = ui.Red
			bad = true
		}
		u.Box(in.Role.Title, body, color)
	}
	if bad {
		return 1
	}
	return 0
}

func statusCard(ctx context.Context, in installed, now time.Time) (string, bool) {
	healthy := true
	want := in.Env.Get(role.KeyVersion)
	rows := [][2]string{{"папка", in.Dir}, {"образы", want}}
	var lines []string
	services, err := compose.Services(ctx, in.Dir)
	if err != nil {
		lines = append(lines, ui.Colored(ui.Red, "✗ compose: "+err.Error()))
		healthy = false
	}
	containers, err := compose.Snapshot(ctx, in.Dir)
	if err != nil {
		lines = append(lines, ui.Colored(ui.Red, "✗ docker: "+err.Error()))
		healthy = false
	}
	byService := map[string]compose.Container{}
	for _, c := range containers {
		byService[c.Service] = c
		if tag, ok := sylphTag(c.Image); ok && tag != want {
			rows[1][1] += ui.Colored(ui.Yellow, " · "+c.Service+" запущен на "+tag)
		}
	}
	verdict := compose.Evaluate(services, containers, now, 0)
	for _, st := range verdict.States {
		c := byService[st.Name]
		line := st.Name + ui.Dimmed(" · "+st.Detail)
		if c.State == "running" && !c.StartedAt.IsZero() {
			line += ui.Dimmed(", " + ago(now.Sub(c.StartedAt)))
		}
		if c.RestartCount > 0 {
			line += ui.Colored(ui.Yellow, fmt.Sprintf(", перезапусков: %d", c.RestartCount))
		}
		switch st.Status {
		case compose.Ready:
			lines = append(lines, ui.Colored(ui.Green, "✓ ")+line)
		default:
			lines = append(lines, ui.Colored(ui.Red, "✗ ")+line)
			healthy = false
		}
	}
	certLines, certsOK := certStatus(in.Dir, now)
	if !certsOK {
		healthy = false
	}
	disk := machine.DiskFree(in.Dir)
	diskLine := disk.Detail
	if disk.Level != machine.OK {
		diskLine = ui.Colored(ui.Yellow, diskLine)
	}
	rows = append(rows, [2]string{"диск", diskLine})
	body := ui.KV(rows) + "\n\n" + ui.Bold("Контейнеры") + "\n" + strings.Join(lines, "\n")
	if len(certLines) > 0 {
		body += "\n\n" + ui.Bold("Серты") + "\n" + strings.Join(certLines, "\n")
	}
	return body, healthy
}

func sylphTag(image string) (string, bool) {
	name, tag, ok := strings.Cut(image, ":")
	if !ok || !strings.HasPrefix(name, "ghcr.io/itg-labs/sylph-") {
		return "", false
	}
	return tag, true
}

func certStatus(dir string, now time.Time) ([]string, bool) {
	root := filepath.Join(dir, "certs")
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".pem") && filepath.Base(path) != "key.pem" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return []string{ui.Colored(ui.Red, "✗ "+err.Error())}, false
	}
	sort.Strings(paths)
	ok := true
	var lines []string
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		data, err := os.ReadFile(path)
		if err != nil {
			lines = append(lines, ui.Colored(ui.Red, "✗ "+rel+": "+err.Error()))
			ok = false
			continue
		}
		cert, err := mtls.ParseCertPEM(data)
		if err != nil {
			lines = append(lines, ui.Colored(ui.Red, "✗ "+rel+": не серт"))
			ok = false
			continue
		}
		left := cert.NotAfter.Sub(now)
		days := int(left.Hours() / 24)
		text := fmt.Sprintf("%s %s · до %s, %d дн.", rel, ui.Dimmed(cert.Subject.CommonName), cert.NotAfter.Local().Format("02.01.2006"), days)
		switch {
		case left < 7*24*time.Hour:
			lines = append(lines, ui.Colored(ui.Red, "✗ ")+text)
			ok = false
		case left < 30*24*time.Hour:
			lines = append(lines, ui.Colored(ui.Yellow, "! ")+text)
		default:
			lines = append(lines, ui.Colored(ui.Green, "✓ ")+text)
		}
	}
	return lines, ok
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "только что"
	case d < time.Hour:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d ч", int(d.Hours()))
	}
	return fmt.Sprintf("%d дн", int(d.Hours()/24))
}
