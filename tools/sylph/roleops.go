package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/machine"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/wizard"
)

var stopWarnings = map[string]string{
	"core":    "ядро и база встанут: панель, бот, subs и ноды потеряют ядро, ноды продолжат пускать по кэшу",
	"node":    "клиенты на этой ноде отключатся",
	"panel":   "панель будет недоступна",
	"bot":     "бот перестанет отвечать, вебхуки ЮKassa не дойдут — оплаты подхватит крон",
	"subs":    "клиенты не смогут обновить подписку",
	"monitor": "алерты перестанут приходить",
}

func cmdStop(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags(u, "stop", "sudo ./sylph stop [роль] [флаги]")
	dir := fs.String("dir", "", "папка роли")
	yes := fs.Bool("yes", false, "не спрашивать подтверждение")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	in, err := pickRole(ctx, u, pos, *dir)
	if err != nil {
		return err
	}
	if !*yes {
		if !u.TTY {
			return errors.New("без терминала останавливаю только с --yes")
		}
		ok, err := wizardFor(u).Confirm("Остановить "+in.Role.Name+"?", stopWarnings[in.Role.Name]+"; запустить снова — start", false)
		if err != nil {
			return err
		}
		if !ok {
			return wizard.ErrAborted
		}
	}
	if _, err := u.Step(ctx, "Остановка "+in.Role.Name, func(ctx context.Context, set func(string)) (string, error) {
		return "контейнеры остановлены, после перезагрузки сами не поднимутся", compose.Stop(ctx, in.Dir)
	}); err != nil {
		return shown(err)
	}
	return nil
}

func cmdStart(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags(u, "start", "sudo ./sylph start [роль] [флаги]")
	dir := fs.String("dir", "", "папка роли")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	in, err := pickRole(ctx, u, pos, *dir)
	if err != nil {
		return err
	}
	if _, err := u.Step(ctx, "Запуск "+in.Role.Name, func(ctx context.Context, set func(string)) (string, error) {
		return "docker compose up -d", compose.Up(ctx, in.Dir)
	}); err != nil {
		return shown(err)
	}
	return waitHealthy(ctx, u, in.Dir)
}

func pickRole(ctx context.Context, u *ui.UI, pos []string, dir string) (installed, error) {
	if err := machine.Root(); err != nil {
		return installed{}, err
	}
	if len(pos) > 1 {
		return installed{}, fmt.Errorf("лишние аргументы: %s", strings.Join(pos[1:], " "))
	}
	roles, err := findRoles(ctx, dir)
	if err != nil {
		return installed{}, err
	}
	if len(roles) == 0 {
		return installed{}, errors.New("ролей sylph на машине не нашёл (ищу в docker compose ls и ./sylph-*); укажи --dir")
	}
	if len(pos) == 1 {
		for _, in := range roles {
			if in.Role.Name == pos[0] {
				return in, nil
			}
		}
		return installed{}, fmt.Errorf("роли %s на машине нет", pos[0])
	}
	if len(roles) == 1 {
		return roles[0], nil
	}
	if !u.TTY {
		return installed{}, errors.New("на машине несколько ролей: укажи, какую")
	}
	labels := make([]string, len(roles))
	for i, in := range roles {
		labels[i] = in.Role.Title + "  " + ui.Dimmed(in.Dir)
	}
	idx, err := wizardFor(u).Select("Какую роль?", labels)
	if err != nil {
		return installed{}, err
	}
	return roles[idx], nil
}
