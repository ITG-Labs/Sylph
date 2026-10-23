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

const certTTL = 365 * 24 * time.Hour

const certsUsage = `sylph certs <команда> — на своём компьютере, рядом с CA (не на сервере)

  bundle <роль> [--name <имя>]   пакет сертов роли; первый запуск создаст CA
  list                           серты CA и их сроки
  revoke <имя>                   отозвать серт`

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
		return errors.New("нужна команда: bundle, list или revoke")
	}
	switch args[0] {
	case "bundle":
		return certsBundle(ctx, u, args[1:])
	case "list":
		return certsList(u, args[1:])
	case "revoke":
		return certsRevoke(u, args[1:])
	case "-h", "--help", "help":
		u.Line(certsUsage)
		return nil
	}
	return fmt.Errorf("неизвестная команда certs %q: bundle, list или revoke", args[0])
}

func certsBundle(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags(u, "certs bundle", "./sylph certs bundle <роль> [флаги]")
	caDir := fs.String("ca", defaultCADir(), "папка CA")
	name := fs.String("name", "", "имя серта ноды, например node-de-1 (только для node)")
	file := fs.Bool("file", false, "вместо строки записать пакет файлом .sylph-certs (для scp)")
	out := fs.String("out", ".", "куда положить файл пакета (с --file)")
	yes := fs.Bool("yes", false, "создать CA без вопроса, если его нет")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("укажи роль: ./sylph certs bundle <core|panel|bot|subs|node|monitor>")
	}
	r, err := role.Get(pos[0])
	if err != nil {
		return err
	}
	wz := wizardFor(u)
	leafName := r.Name
	switch {
	case r.Name == "node" && *name == "" && u.TTY:
		if *name, err = wz.Input("Имя ноды", "оно же cert_cn ноды в панели, например node-de-1", castore.ValidName); err != nil {
			return err
		}
		leafName = *name
	case r.Name == "node" && *name == "":
		return errors.New("для ноды нужно имя: --name node-de-1")
	case r.Name == "node":
		leafName = *name
	case *name != "":
		return errors.New("--name только для node: у остальных ролей имя серта — имя роли")
	}
	if err := castore.ValidName(leafName); err != nil {
		return err
	}

	u.Banner("пакет сертов", "роль "+r.Name+" · CA "+*caDir)
	if err := ensureCA(ctx, u, wz, *caDir, *yes); err != nil {
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
			u.Info("серт " + n + " уже есть — беру его")
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
		u.OK("выпущен серт " + n + " до " + now.Add(certTTL).Format("02.01.2006"))
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
		lines = append(lines, "revoked.json"+ui.Dimmed(" · список отозванных"))
	}
	inside := ui.KV([][2]string{{"внутри", strings.Join(lines, "\n        ")}})
	keyNote := "\n\n" + ui.Dimmed("ca.key остаётся здесь, в "+*caDir+": на машины он не попадает")
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
			"\n\n"+ui.Bold("Довезти: ")+"scp "+shown+" root@<машина>:"+
			"\n"+ui.Dimmed("на машине: sudo sylph install "+r.Name+" --certs ~/"+filepath.Base(path))+keyNote, ui.Green)
		return nil
	}
	line, err := b.Inline()
	if err != nil {
		return err
	}
	clip := "строка ниже"
	if u.Clipboard(line) {
		clip = "строка ниже (и в буфере обмена, если терминал умеет)"
	}
	u.Box("Пакет "+r.Name+" готов — "+clip, inside+
		"\n\n"+ui.Bold("Дальше: ")+"на машине роли sudo sylph → /install "+r.Name+" → в шаг «Пакет сертов» вставь строку"+
		"\n"+ui.Dimmed("в строке приватный ключ роли — не пересылай её в чаты; после установки она больше не нужна")+keyNote, ui.Green)
	u.Line(line)
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
		return errors.New("CA создаю только на своём компьютере и без sudo: ключ CA не должен жить на сервере")
	}
	if projects, err := compose.Ls(ctx); err == nil {
		for _, p := range projects {
			if strings.HasPrefix(p.Name, "sylph-") {
				return fmt.Errorf("на этой машине стоит роль (%s): CA создают на своём компьютере, а не на сервере", p.Name)
			}
		}
	}
	if !yes {
		if !u.TTY {
			return fmt.Errorf("CA в %s нет: создать новый — флаг --yes", dir)
		}
		create, err := wz.Confirm("Создать новый боевой CA в "+dir+"?", "CA ещё нет. Его ключ ca.key останется только здесь, на вашем компьютере; все серты ролей будут подписаны им", true)
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
	u.OK("создан CA в " + dir + " — сделай копию ca.key офлайн, без него новых сертов не выпустить")
	return nil
}

func certsList(u *ui.UI, args []string) error {
	fs := newFlags(u, "certs list", "./sylph certs list [флаги]")
	caDir := fs.String("ca", defaultCADir(), "папка CA")
	if _, err := parseWithArgs(fs, args); err != nil {
		return err
	}
	ok, err := castore.Exists(*caDir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("CA в %s нет: его создаст первый `./sylph certs bundle <роль>`", *caDir)
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
		u.Info("в CA пока нет сертов")
		return nil
	}
	u.Box("Серты CA "+*caDir, ui.KV(rows), ui.Accent)
	return nil
}

func certsRevoke(u *ui.UI, args []string) error {
	fs := newFlags(u, "certs revoke", "./sylph certs revoke <имя> [флаги]")
	caDir := fs.String("ca", defaultCADir(), "папка CA")
	yes := fs.Bool("yes", false, "не спрашивать подтверждение")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("укажи имя серта: ./sylph certs revoke <имя> (имена — в ./sylph certs list)")
	}
	if !*yes {
		if !u.TTY {
			return errors.New("без терминала отзываю только с --yes")
		}
		ok, err := wizardFor(u).Confirm("Отозвать серт "+pos[0]+"?", "ядро перестанет его пускать после перезапуска с новым revoked.json; вернуть нельзя", false)
		if err != nil {
			return err
		}
		if !ok {
			return wizard.ErrAborted
		}
	}
	if err := castore.Revoke(*caDir, pos[0]); err != nil {
		return err
	}
	u.OK("серт " + pos[0] + " отозван")
	u.Info("дальше: sylph certs bundle core → на машине ядра sudo sylph install core и вставь новый пакет")
	return nil
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
