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
	"core":    "ядро и база данных будут остановлены: панель, бот, subs и ноды потеряют связь с ядром, ноды продолжат обслуживать клиентов по кэшу",
	"node":    "клиенты этой ноды будут отключены",
	"panel":   "панель будет недоступна",
	"bot":     "бот перестанет отвечать, уведомления ЮKassa не будут доставлены; оплаты обработает cron",
	"subs":    "клиенты не смогут обновить подписку",
	"monitor": "уведомления мониторинга перестанут приходить",
}

func cmdStop(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags(u, "stop", "sudo ./sylph stop [роль] [флаги]")
	dir := fs.String("dir", "", "каталог роли")
	yes := fs.Bool("yes", false, "не запрашивать подтверждение")
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
			return errors.New("без терминала остановка выполняется только с флагом --yes")
		}
		ok, err := wizardFor(u).Confirm("Остановить "+in.Role.Name+"?", stopWarnings[in.Role.Name]+". Для запуска используйте start", false)
		if err != nil {
			return err
		}
		if !ok {
			return wizard.ErrAborted
		}
	}
	if _, err := u.Step(ctx, "Остановка "+in.Role.Name, func(ctx context.Context, set func(string)) (string, error) {
		return "контейнеры остановлены и не запустятся автоматически после перезагрузки", compose.Stop(ctx, in.Dir)
	}); err != nil {
		return shown(err)
	}
	return nil
}

func cmdStart(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags(u, "start", "sudo ./sylph start [роль] [флаги]")
	dir := fs.String("dir", "", "каталог роли")
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
		return installed{}, errors.New("роли sylph на сервере не найдены (поиск через docker compose ls); укажите каталог роли через --dir")
	}
	if len(pos) == 1 {
		for _, in := range roles {
			if in.Role.Name == pos[0] {
				return in, nil
			}
		}
		return installed{}, fmt.Errorf("роль %s на сервере не установлена", pos[0])
	}
	if len(roles) == 1 {
		return roles[0], nil
	}
	if !u.TTY {
		return installed{}, errors.New("на сервере несколько ролей: укажите нужную")
	}
	labels := make([]string, len(roles))
	for i, in := range roles {
		labels[i] = in.Role.Title + "  " + ui.Dimmed(in.Dir)
	}
	idx, err := wizardFor(u).Select("Выберите роль", labels)
	if err != nil {
		return installed{}, err
	}
	return roles[idx], nil
}
