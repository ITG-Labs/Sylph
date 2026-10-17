package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	_ "time/tzdata"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/wizard"
)

const usage = `sylph — установка и обслуживание ролей Sylph

На машине роли (от root):
  sudo ./sylph install [роль]   мастер: настройки, серты, запуск, ожидание здоровья
  sudo ./sylph update           обновить роли машины до версии этого бинаря
  sudo ./sylph status           здоровье ролей машины
  sudo ./sylph start [роль]     запустить роль и дождаться здоровья
  sudo ./sylph stop [роль]      остановить роль
  sudo ./sylph                  всё то же в интерактивном режиме

На ноуте рядом с CA:
  ./sylph certs bundle <роль>   пакет сертов роли (первый запуск создаст CA)
  ./sylph certs list            серты CA и их сроки
  ./sylph certs revoke <имя>    отозвать серт

  ./sylph version               версия бинаря и образов

Роли: core, panel, bot, subs, node, monitor.
Подробности по флагам: sylph <команда> -h`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string) int {
	u := ui.New()
	if len(args) == 0 {
		if u.TTY {
			return runShell(ctx, u)
		}
		u.Line(usage)
		return 2
	}
	return dispatch(ctx, u, args)
}

func dispatch(ctx context.Context, u *ui.UI, args []string) int {
	var err error
	switch args[0] {
	case "install":
		err = cmdInstall(ctx, u, args[1:])
	case "update":
		err = cmdUpdate(ctx, u, args[1:])
	case "status":
		return cmdStatus(ctx, u, args[1:])
	case "start":
		err = cmdStart(ctx, u, args[1:])
	case "stop":
		err = cmdStop(ctx, u, args[1:])
	case "certs":
		err = cmdCerts(ctx, u, args[1:])
	case "version", "--version", "-v":
		u.Line(fmt.Sprintf("sylph %s (образы: %s)", version.Version, imageVersionOrDev()))
		return 0
	case "help", "-h", "--help":
		u.Line(usage)
		return 0
	default:
		u.Fail(fmt.Sprintf("неизвестная команда %q", args[0]))
		u.Line(usage)
		return 2
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, wizard.ErrAborted), errors.Is(err, context.Canceled):
		u.Warn("отменено")
		return 130
	}
	var shown shownError
	if !errors.As(err, &shown) {
		u.Fail(err.Error())
	}
	return 1
}

type shownError struct{ error }

func (s shownError) Unwrap() error { return s.error }

func shown(err error) error { return shownError{err} }

var tagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

func imageVersion(override string) (string, error) {
	if override != "" {
		if !tagPattern.MatchString(override) {
			return "", fmt.Errorf("--version %q: не похоже на тег образа", override)
		}
		return override, nil
	}
	v := version.Version
	switch {
	case v == "" || v == "dev":
		return "", errors.New("это локальная сборка без версии: укажи тег образов через --version (например 0.1.0)")
	case strings.HasPrefix(v, "edge-"):
		return "sha-" + strings.TrimPrefix(v, "edge-"), nil
	}
	return v, nil
}

func imageVersionOrDev() string {
	v, err := imageVersion("")
	if err != nil {
		return "не заданы"
	}
	return v
}

func wizardFor(u *ui.UI) wizard.Wizard {
	return wizard.Wizard{Run: u.Form}
}

func newFlags(u *ui.UI, name string, short string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(u.Writer())
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "%s\n\nФлаги:\n", short)
		fs.PrintDefaults()
	}
	return fs
}

func parseWithArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}
