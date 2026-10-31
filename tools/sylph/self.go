package main

import (
	"context"
	"errors"
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

func (r *restartRequest) Error() string { return "sylph обновлён: требуется перезапуск новой версии" }

func (r *restartRequest) run(u *ui.UI) int {
	env := []string{envSelfUpdated + "=" + r.from}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, envSelfUpdated+"=") {
			env = append(env, kv)
		}
	}
	err := execve(r.exe, append([]string{r.exe}, r.args...), env)
	u.Fail("новая версия sylph установлена, но не запустилась: " + err.Error() + ". Запустите sudo ./sylph update повторно")
	return 1
}

func selfUpdate(ctx context.Context, u *ui.UI, args []string) error {
	if from := os.Getenv(envSelfUpdated); from != "" {
		u.OK("sylph обновлён: " + from + " → " + version.Version)
		return nil
	}
	cur := version.Version
	if _, ok := semver(cur); !ok {
		u.Info("сборка " + cur + " не поддерживает самообновление: оно доступно только для релизов")
		return nil
	}
	c := releases()
	var latest string
	if _, err := u.Step(ctx, "Новый sylph", func(ctx context.Context, set func(string)) (string, error) {
		set("запрос к GitHub")
		v, err := c.Latest(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", ui.WarnError{Msg: "не удалось проверить (" + err.Error() + "), обновление до " + cur}
		}
		if !older(cur, v) {
			return "не требуется, " + cur + " — последняя версия", nil
		}
		latest = v
		return "доступна " + v, nil
	}); err != nil && !ui.IsWarn(err) {
		return err
	}
	if latest == "" {
		return nil
	}
	exe, err := executable()
	if err != nil {
		u.Warn("не удалось найти исполняемый файл sylph (" + err.Error() + "): sylph не обновляется, роли обновляются до " + cur)
		return nil
	}
	if _, err := u.Step(ctx, "sylph "+latest, func(ctx context.Context, set func(string)) (string, error) {
		set("загрузка")
		tmp, err := c.Fetch(ctx, latest, filepath.Dir(exe))
		if err != nil {
			return "", err
		}
		set("проверка и замена")
		if err := selfupdate.Install(ctx, tmp, exe, latest); err != nil {
			return "", err
		}
		return "SHA256 совпадает · " + exe, nil
	}); err != nil {
		if !errors.Is(err, context.Canceled) {
			u.Info("роли можно обновить без обновления sylph: sudo ./sylph update --no-self")
		}
		return shown(err)
	}
	return &restartRequest{exe: exe, args: args, from: cur}
}
