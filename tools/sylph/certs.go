package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/pkg/castore"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/bundle"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/wizard"
)

const (
	certTTL        = 365 * 24 * time.Hour
	certWarnBefore = 30 * 24 * time.Hour
)

const certsUsage = `sylph certs <команда> — выполняется на компьютере с CA (не на сервере)

  bundle <роль> [--name <имя>]   пакет сертификатов роли; при первом запуске создаётся CA
  renew <роль> [--name <имя>]    выпустить сертификат роли заново (истекает или отозван); старый уходит в архив
  list                           сертификаты CA и сроки их действия
  revoke <имя>                   отозвать сертификат`

func defaultCADir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "sylph-ca"
	}
	return filepath.Join(home, "sylph-ca")
}

func cmdCerts(ctx context.Context, u *ui.UI, args []string) error {
	if len(args) == 0 {
		u.Line(certsUsage)
		return errors.New("укажите команду: bundle, renew, list или revoke")
	}
	switch args[0] {
	case "bundle":
		return certsBundle(ctx, u, args[1:])
	case "renew":
		return certsRenew(u, args[1:])
	case "list":
		return certsList(u, args[1:])
	case "revoke":
		return certsRevoke(u, args[1:])
	case "-h", "--help", "help":
		u.Line(certsUsage)
		return nil
	}
	return fmt.Errorf("неизвестная команда certs %q: допустимы bundle, renew, list и revoke", args[0])
}

func certsBundle(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags(u, "certs bundle", "./sylph certs bundle <роль> [флаги]")
	caDir := fs.String("ca", defaultCADir(), "каталог CA")
	name := fs.String("name", "", "имя сертификата ноды, например node-1 (только для node)")
	file := fs.Bool("file", false, "записать пакет в файл .sylph-certs вместо вывода строки (для scp)")
	out := fs.String("out", ".", "каталог для файла пакета (с --file)")
	yes := fs.Bool("yes", false, "создать CA без подтверждения, если он отсутствует")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("укажите роль: ./sylph certs bundle <core|panel|bot|subs|node|monitor>")
	}
	r, err := role.Get(pos[0])
	if err != nil {
		return err
	}
	leafName, err := resolveLeafName(u, r, *name)
	if err != nil {
		return err
	}

	u.Banner("пакет сертификатов", "роль "+r.Name+" · CA "+*caDir)
	if err := ensureCA(ctx, u, wizardFor(u), *caDir, *yes); err != nil {
		return err
	}
	now := time.Now()
	names := bundle.LeafNames(r.Name, leafName, role.DefaultCoreName)
	for _, n := range names {
		has, err := castore.HasLeaf(*caDir, n)
		if err != nil {
			return err
		}
		if has {
			warn, err := checkLeaf(*caDir, n, renewCommand(r.Name, leafName), now)
			if err != nil {
				return err
			}
			if warn != "" {
				u.Warn(warn)
			}
			u.Info("сертификат " + n + " уже существует и будет использован")
			continue
		}
		usage, certRole := mtls.ClientUsage, r.Name
		switch {
		case r.Name == "core" && n == role.DefaultCoreName:
			usage, certRole = mtls.ServerUsage, ""
		case r.Name == "core":
			certRole = mtls.RoleCron
		}
		if err := castore.Issue(*caDir, n, usage, certRole, certTTL, now); err != nil {
			return err
		}
		u.OK("выпущен сертификат " + n + " до " + now.Add(certTTL).Format("02.01.2006"))
	}
	b, err := bundle.Collect(*caDir, r.Name, leafName, role.DefaultCoreName, now)
	if err != nil {
		return err
	}
	leaves, err := b.Verify(r.Name, role.DefaultCoreName, now)
	if err != nil {
		return err
	}
	var lines []string
	for _, l := range leaves {
		lines = append(lines, l.Name+ui.Dimmed(" · до "+l.NotAfter.Local().Format("02.01.2006")))
	}
	if _, ok := b.Files[bundle.RevokedFile]; ok {
		lines = append(lines, "revoked.json"+ui.Dimmed(" · список отозванных сертификатов"))
	}
	inside := ui.KV([][2]string{{"состав", strings.Join(lines, "\n        ")}})
	keyNote := "\n\n" + ui.Dimmed("ca.key остаётся в "+*caDir+" и не передаётся на серверы")
	if *file {
		data, err := b.Encode()
		if err != nil {
			return err
		}
		fileName := leafName
		if r.Name == "core" {
			fileName = "core"
		}
		path, err := filepath.Abs(filepath.Join(*out, fileName+bundle.Ext))
		if err != nil {
			return err
		}
		if err := writeFileAtomic(path, data, 0o600); err != nil {
			return err
		}
		shown := displayPath(path)
		u.Box("Пакет "+r.Name+" готов", ui.KV([][2]string{{"файл", shown}})+"\n"+inside+
			"\n\n"+ui.Bold("Передача: ")+"scp "+shown+" root@<сервер>:"+
			"\n"+ui.Dimmed("на сервере: sudo sylph install "+r.Name+" --certs ~/"+filepath.Base(path))+keyNote, ui.Green)
		return nil
	}
	line, err := b.Inline()
	if err != nil {
		return err
	}
	clip := "строка ниже"
	if u.Clipboard(line) {
		clip = "строка ниже (также скопирована в буфер обмена, если терминал это поддерживает)"
	}
	u.Box("Пакет "+r.Name+" готов: "+clip, inside+
		"\n\n"+ui.Bold("Далее: ")+"на сервере роли выполните sudo sylph → /install "+r.Name+" и вставьте строку на шаге «Пакет сертификатов»"+
		"\n"+ui.Dimmed("строка содержит приватный ключ роли: не пересылайте её через мессенджеры; после установки она больше не нужна")+keyNote, ui.Green)
	u.Line(line)
	return nil
}

func resolveLeafName(u *ui.UI, r *role.Role, name string) (string, error) {
	leafName := r.Name
	switch {
	case r.Name == "node" && name == "" && u.TTY:
		var err error
		if name, err = wizardFor(u).Input("Имя ноды", "используется как cert_cn ноды в панели, например node-1", castore.ValidName); err != nil {
			return "", err
		}
		leafName = name
	case r.Name == "node" && name == "":
		return "", errors.New("для ноды укажите имя: --name node-1")
	case r.Name == "node":
		leafName = name
	case name != "":
		return "", errors.New("--name используется только для node: у остальных ролей имя сертификата совпадает с именем роли")
	}
	if err := castore.ValidName(leafName); err != nil {
		return "", err
	}
	return leafName, nil
}

func renewCommand(roleName string, leafName string) string {
	cmd := "sylph certs renew " + roleName
	if roleName == "node" {
		cmd += " --name " + leafName
	}
	return cmd
}

func checkLeaf(caDir string, name string, renewCmd string, now time.Time) (string, error) {
	info, err := castore.Inspect(caDir, name)
	if err != nil {
		return "", err
	}
	revoked, err := castore.IsRevoked(caDir, info.Serial)
	if err != nil {
		return "", err
	}
	if revoked {
		return "", fmt.Errorf("сертификат %s отозван и в пакет не попадёт: выпустите новый командой `%s`", name, renewCmd)
	}
	left := info.NotAfter.Sub(now)
	switch {
	case left <= 0:
		return "", fmt.Errorf("срок действия сертификата %s истёк %s: выпустите новый командой `%s`", name, info.NotAfter.Local().Format("02.01.2006"), renewCmd)
	case left < certWarnBefore:
		return fmt.Sprintf("сертификат %s истекает %s (осталось %d дн.): выпустите новый командой `%s`", name, info.NotAfter.Local().Format("02.01.2006"), int(left.Hours()/24), renewCmd), nil
	}
	return "", nil
}

func certsRenew(u *ui.UI, args []string) error {
	fs := newFlags(u, "certs renew", "./sylph certs renew <роль> [флаги]")
	caDir := fs.String("ca", defaultCADir(), "каталог CA")
	name := fs.String("name", "", "имя сертификата ноды, например node-1 (только для node)")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("укажите роль: ./sylph certs renew <core|panel|bot|subs|node|monitor>")
	}
	r, err := role.Get(pos[0])
	if err != nil {
		return err
	}
	leafName, err := resolveLeafName(u, r, *name)
	if err != nil {
		return err
	}
	ok, err := castore.Exists(*caDir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("CA в %s не найден: перевыпускать нечего", *caDir)
	}
	names := bundle.LeafNames(r.Name, leafName, role.DefaultCoreName)
	for _, n := range names {
		has, err := castore.HasLeaf(*caDir, n)
		if err != nil {
			return err
		}
		if !has {
			return fmt.Errorf("сертификат %s не найден: сначала выпустите его командой `sylph certs bundle %s`", n, r.Name)
		}
	}
	now := time.Now()
	for _, n := range names {
		if err := castore.Renew(*caDir, n, certTTL, now); err != nil {
			return err
		}
		u.OK("выпущен новый сертификат " + n + " до " + now.Add(certTTL).Format("02.01.2006") + ", прежний перенесён в " + filepath.Join(*caDir, "archive"))
	}
	next := "далее: sylph certs bundle " + r.Name
	if r.Name == "node" {
		next += " --name " + leafName
	}
	next += ", затем на сервере роли sudo sylph install " + r.Name + " с новым пакетом"
	if r.Name != "core" {
		next += "; если старый сертификат был отозван, ядру нужен свежий revoked.json: sylph certs bundle core и sudo sylph install core"
	}
	u.Info(next)
	return nil
}

func ensureCA(ctx context.Context, u *ui.UI, wz wizard.Wizard, dir string, yes bool) error {
	ok, err := castore.Exists(dir)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	if os.Geteuid() == 0 {
		return errors.New("CA создаётся только на компьютере администратора и без sudo: ключ CA не должен храниться на сервере")
	}
	if projects, err := compose.Ls(ctx); err == nil {
		for _, p := range projects {
			if strings.HasPrefix(p.Name, "sylph-") {
				return fmt.Errorf("на этом компьютере установлена роль (%s): CA создаётся на компьютере администратора, а не на сервере", p.Name)
			}
		}
	}
	if !yes {
		if !u.TTY {
			return fmt.Errorf("CA в %s не найден: для создания нового используйте флаг --yes", dir)
		}
		create, err := wz.Confirm("Создать новый рабочий CA в "+dir+"?", "CA ещё не создан. Его ключ ca.key будет храниться только на этом компьютере; им будут подписаны все сертификаты ролей", true)
		if err != nil {
			return err
		}
		if !create {
			return wizard.ErrAborted
		}
	}
	if err := castore.Init(dir, "Sylph CA", time.Now()); err != nil {
		return err
	}
	u.OK("создан CA в " + dir + ". Сохраните резервную копию ca.key на внешнем носителе: без него выпуск новых сертификатов невозможен")
	return nil
}

func certsList(u *ui.UI, args []string) error {
	fs := newFlags(u, "certs list", "./sylph certs list [флаги]")
	caDir := fs.String("ca", defaultCADir(), "каталог CA")
	if _, err := parseWithArgs(fs, args); err != nil {
		return err
	}
	ok, err := castore.Exists(*caDir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("CA в %s не найден: он будет создан при первом запуске `./sylph certs bundle <роль>`", *caDir)
	}
	infos, err := castore.List(*caDir)
	if err != nil {
		return err
	}
	revoked, err := castore.LoadRevoked(*caDir)
	if err != nil {
		return err
	}
	isRevoked := map[string]bool{}
	for _, s := range revoked {
		isRevoked[s] = true
	}
	now := time.Now()
	var rows [][2]string
	for _, ci := range infos {
		roleName := ci.Role
		if roleName == "" {
			roleName = ci.Type
		}
		left := ci.NotAfter.Sub(now)
		when := fmt.Sprintf("%s · до %s (%d дн.)", roleName, ci.NotAfter.Local().Format("02.01.2006"), int(left.Hours()/24))
		switch {
		case isRevoked[ci.Serial]:
			when = ui.Dimmed(when + " · отозван")
		case left < 7*24*time.Hour:
			when = ui.Colored(ui.Red, when)
		case left < 30*24*time.Hour:
			when = ui.Colored(ui.Yellow, when)
		}
		rows = append(rows, [2]string{ci.Name, when})
	}
	if len(rows) == 0 {
		u.Info("в CA пока нет сертификатов")
		return nil
	}
	u.Box("Сертификаты CA "+*caDir, ui.KV(rows), ui.Accent)
	return nil
}

func certsRevoke(u *ui.UI, args []string) error {
	fs := newFlags(u, "certs revoke", "./sylph certs revoke <имя> [флаги]")
	caDir := fs.String("ca", defaultCADir(), "каталог CA")
	yes := fs.Bool("yes", false, "не запрашивать подтверждение")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("укажите имя сертификата: ./sylph certs revoke <имя> (список имён: ./sylph certs list)")
	}
	if !*yes {
		if !u.TTY {
			return errors.New("без терминала отзыв выполняется только с флагом --yes")
		}
		ok, err := wizardFor(u).Confirm("Отозвать сертификат "+pos[0]+"?", "ядро перестанет принимать его после перезапуска с новым revoked.json; отмена отзыва невозможна", false)
		if err != nil {
			return err
		}
		if !ok {
			return wizard.ErrAborted
		}
	}
	info, infoErr := castore.Inspect(*caDir, pos[0])
	if err := castore.Revoke(*caDir, pos[0]); err != nil {
		return err
	}
	u.OK("сертификат " + pos[0] + " отозван")
	u.Info("далее: выполните sylph certs bundle core, затем на сервере ядра sudo sylph install core и вставьте новый пакет")
	if infoErr == nil {
		u.Info("вернуть эту роль в строй: `" + renewHint(info) + "`, затем sylph certs bundle и установка на сервере")
	}
	return nil
}

func renewHint(info castore.CertInfo) string {
	switch {
	case info.Type == "server" || info.Role == mtls.RoleCron:
		return "sylph certs renew core"
	case info.Role == mtls.RoleNode:
		return "sylph certs renew node --name " + info.Name
	}
	return "sylph certs renew " + info.Role
}

func displayPath(path string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}
