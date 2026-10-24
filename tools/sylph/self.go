package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/selfupdate"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
)

const envSelfUpdated = "SYLPH_SELF_UPDATED"

var (
	releases   = func() *selfupdate.Client { return selfupdate.New(version.Version) }
	executable = func() (string, error) {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(exe)
	}
	execve = syscall.Exec
)

type restartRequest struct {
	exe  string
	args []string
	from string
}

func (r *restartRequest) Error() string { return "sylph обновился: нужен перезапуск новой версией" }

func (r *restartRequest) run(u *ui.UI) int {
	env := []string{envSelfUpdated + "=" + r.from}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, envSelfUpdated+"=") {
			env = append(env, kv)
		}
	}
	err := execve(r.exe, append([]string{r.exe}, r.args...), env)
	u.Fail("новый sylph на месте, но сам не запустился: " + err.Error() + " — запусти sudo ./sylph update ещё раз")
	return 1
}

func selfUpdate(ctx context.Context, u *ui.UI, args []string) error {
	if from := os.Getenv(envSelfUpdated); from != "" {
		u.OK("sylph обновлён: " + from + " → " + version.Version)
		return nil
	}
	cur := version.Version
	if _, ok := semver(cur); !ok {
		u.Info("сборка " + cur + " сама себя не обновляет — это умеют только релизы")
		return nil
	}
	c := releases()
	var latest string
	if _, err := u.Step(ctx, "Новый sylph", func(ctx context.Context, set func(string)) (string, error) {
		set("спрашиваю GitHub")
		v, err := c.Latest(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", ui.WarnError{Msg: "не смог проверить (" + err.Error() + "), обновляю до " + cur}
		}
		if !older(cur, v) {
			return "нет, " + cur + " — последний", nil
		}
		latest = v
		return "вышел " + v, nil
	}); err != nil && !ui.IsWarn(err) {
		return err
	}
	if latest == "" {
		return nil
	}
	exe, err := executable()
	if err != nil {
		return fmt.Errorf("не нашёл свой бинарь: %w", err)
	}
	if _, err := u.Step(ctx, "sylph "+latest, func(ctx context.Context, set func(string)) (string, error) {
		set("качаю")
		tmp, err := c.Fetch(ctx, latest, filepath.Dir(exe))
		if err != nil {
			return "", err
		}
		set("проверяю и подменяю")
		if err := selfupdate.Install(ctx, tmp, exe, latest); err != nil {
			return "", err
		}
		return "SHA256 сошёлся · " + exe, nil
	}); err != nil {
		return shown(err)
	}
	return &restartRequest{exe: exe, args: args, from: cur}
}
