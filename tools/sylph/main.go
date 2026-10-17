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
		fmt.Println(usage)
		return 2
	}
	var err error
	switch args[0] {
	case "install":
		err = cmdInstall(ctx, u, args[1:])
	case "update":
		err = cmdUpdate(ctx, u, args[1:])
	case "status":
		return cmdStatus(ctx, u, args[1:])
	case "certs":
		err = cmdCerts(ctx, u, args[1:])
	case "version", "--version", "-v":
		fmt.Printf("sylph %s (образы: %s)\n", version.Version, imageVersionOrDev())
		return 0
	case "help", "-h", "--help":
		fmt.Println(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "неизвестная команда %q\n\n%s\n", args[0], usage)
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

func newFlags(name string, short string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
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
