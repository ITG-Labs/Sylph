package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/machine"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/wizard"
)

func cmdUpdate(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags(u, "update", "sudo ./sylph update [флаги]")
	dir := fs.String("dir", "", "обновить только роль из этой папки")
	yes := fs.Bool("yes", false, "не спрашивать подтверждение")
	ver := fs.String("version", "", "тег образов вместо версии бинаря")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("лишние аргументы: %s", strings.Join(pos, " "))
	}
	if err := machine.Root(); err != nil {
		return err
	}
	target, err := imageVersion(*ver)
	if err != nil {
		return err
	}
	if !u.TTY && !*yes {
		return errors.New("без терминала обновляю только с --yes")
	}
	u.Banner("обновление", "до образов "+target)
	u.Section("Docker")
	if err := dockerChecks(ctx, u); err != nil {
		return shown(err)
	}
	roles, err := findRoles(ctx, *dir)
	if err != nil {
		return err
	}
	if len(roles) == 0 {
		return errors.New("ролей sylph на машине не нашёл (ищу через docker compose ls); укажи папку роли через --dir")
	}

	var todo []installed
	var rows [][2]string
	downgrade := false
	for _, in := range roles {
		cur := in.Env.Get(role.KeyVersion)
		mark := cur + " → " + target
		switch {
		case cur == target && runningAt(ctx, in.Dir, target):
			mark = cur + ui.Dimmed(" (уже)")
		case older(target, cur):
			mark += ui.Colored(ui.Yellow, " (понижение)")
			downgrade = true
			todo = append(todo, in)
		default:
			todo = append(todo, in)
		}
		rows = append(rows, [2]string{in.Role.Name, mark + ui.Dimmed("  "+in.Dir)})
	}
	u.Section("Роли на машине")
	u.Box("Обновление", ui.KV(rows)+"\n\n"+ui.Dimmed("порядок по машинам: ядро → ноды → панель → бот и subs → монитор"), ui.Accent)
	if len(todo) == 0 {
		u.OK("всё уже на " + target)
		return nil
	}
	if downgrade {
		u.Warn("понижение версии: миграции базы ядра назад не откатываются")
	}
	if u.TTY && !*yes {
		ok, err := wizardFor(u).Confirm("Обновляем?", "свежие compose-файлы, новая версия в .env, pull, up, ожидание здоровья", true)
		if err != nil {
			return err
		}
		if !ok {
			return wizard.ErrAborted
		}
	}

	for _, in := range todo {
		u.Section(in.Role.Title)
		if err := updateRole(ctx, u, in, target); err != nil {
			return err
		}
	}
	u.Line("")
	u.Box("Готово 🎉", "обновлено ролей: "+strconv.Itoa(len(todo))+" · образы "+target+"\nоткат — `sudo ./sylph update` бинарём прежней версии", ui.Green)
	return nil
}

func updateRole(ctx context.Context, u *ui.UI, in installed, target string) error {
	defaults, _, err := in.Role.Defaults()
	if err != nil {
		return err
	}
	env := defaults.Clone()
	env.Merge(in.Env)
	env.Set(role.KeyVersion, target)
	if errs := in.Role.Validate(env); len(errs) > 0 {
		for _, e := range errs {
			u.Fail(e.Error())
		}
		return shown(fmt.Errorf("в новой версии роли %s есть новые обязательные настройки — запусти `sudo ./sylph install %s --dir %s`", in.Role.Name, in.Role.Name, in.Dir))
	}
	if _, err := u.Step(ctx, "Файлы роли", func(ctx context.Context, set func(string)) (string, error) {
		backups, err := writeDeployFiles(in.Dir, in.Role)
		if err != nil {
			return "", err
		}
		if err := prepareEgress(in.Dir, in.Role, env); err != nil {
			return "", err
		}
		if err := writeEnv(in.Dir, in.Role, env); err != nil {
			return "", err
		}
		if len(backups) > 0 {
			return "прежние: " + strings.Join(backups, ", "), nil
		}
		return "compose без изменений", nil
	}); err != nil {
		return shown(err)
	}
	if _, err := u.Step(ctx, "Образы", func(ctx context.Context, set func(string)) (string, error) {
		set("тяну " + target)
		return "на месте", compose.Pull(ctx, in.Dir)
	}); err != nil {
		return shown(err)
	}
	if _, err := u.Step(ctx, "Запуск", func(ctx context.Context, set func(string)) (string, error) {
		return "docker compose up -d", compose.Up(ctx, in.Dir)
	}); err != nil {
		return shown(err)
	}
	return waitHealthy(ctx, u, in.Dir)
}

func older(a string, b string) bool {
	pa, oka := semver(a)
	pb, okb := semver(b)
	if !oka || !okb {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func semver(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
