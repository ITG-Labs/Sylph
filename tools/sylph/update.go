package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/envfile"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/machine"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/wizard"
)

func cmdUpdate(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags(u, "update", "sudo ./sylph update [флаги]")
	dir := fs.String("dir", "", "обновить только роль из этого каталога")
	yes := fs.Bool("yes", false, "не запрашивать подтверждение")
	ver := fs.String("version", "", "тег образов вместо версии sylph (в этом случае sylph не обновляется)")
	noSelf := fs.Bool("no-self", false, "не загружать новый sylph: обновить роли до версии текущего sylph")
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
	if !u.TTY && !*yes {
		return errors.New("без терминала обновление выполняется только с флагом --yes")
	}
	u.Banner("обновление", "sylph "+version.Version)
	if *ver == "" && !*noSelf {
		if err := selfUpdate(ctx, u, append([]string{"update"}, args...)); err != nil {
			return err
		}
	}
	target, err := imageVersion(*ver)
	if err != nil {
		return err
	}
	u.Section("Docker")
	if err := dockerChecks(ctx, u); err != nil {
		return shown(err)
	}
	roles, err := findRoles(ctx, *dir)
	if err != nil {
		return err
	}
	if len(roles) == 0 {
		return errors.New("роли sylph на сервере не найдены (поиск через docker compose ls); укажите каталог роли через --dir")
	}

	var todo []installed
	marks := map[string]string{}
	downgrade := false
	for _, in := range roles {
		cur := in.Env.Get(role.KeyVersion)
		mark := cur + " → " + target
		switch {
		case cur == target && in.Env.Get(role.KeyCaddyVersion) == pinnedCaddy(in.Role) && runningAt(ctx, in.Dir, target, pinnedCaddy(in.Role)):
			mark = cur + ui.Dimmed(" (текущая)")
		case older(target, cur):
			mark += ui.Colored(ui.Yellow, " (понижение)")
			downgrade = true
			todo = append(todo, in)
		default:
			todo = append(todo, in)
		}
		marks[in.Dir] = mark
	}

	var plans []*rolePlan
	defer func() {
		for _, p := range plans {
			p.cleanup()
		}
	}()
	if len(todo) > 0 {
		u.Section("Подготовка")
		for _, in := range todo {
			p, err := prepareUpdate(ctx, u, in, target)
			if p != nil {
				plans = append(plans, p)
			}
			if err != nil {
				return err
			}
			marks[in.Dir] += ui.Dimmed(" · " + p.impact())
		}
	}

	var rows [][2]string
	for _, in := range roles {
		rows = append(rows, [2]string{in.Role.Name, marks[in.Dir] + ui.Dimmed("  "+in.Dir)})
	}
	u.Section("Роли на сервере")
	u.Box("Обновление", ui.KV(rows)+"\n\n"+ui.Dimmed("рекомендуемый порядок обновления серверов: ядро → ноды → панель → бот и subs → монитор"), ui.Accent)
	if len(todo) == 0 {
		u.OK("все роли уже на версии " + target)
		return nil
	}
	if downgrade {
		u.Warn("понижение версии: миграции базы данных ядра не откатываются; ядро старой версии запустится на более новой схеме и запишет предупреждение в лог, но совместимость схемы не гарантируется, при сбоях верните базу из дампа")
	}
	cut := false
	for _, p := range plans {
		if msg, ok := p.cutWarning(); ok {
			u.Warn(msg)
			cut = true
		}
	}
	if u.TTY && !*yes {
		desc := "обновление compose-файлов, новая версия в .env, запуск и проверка состояния"
		if cut {
			desc = "⚠️ подключения клиентов на ноде будут прерваны · " + desc
		}
		ok, err := wizardFor(u).Confirm("Выполнить обновление?", desc, true)
		if err != nil {
			return err
		}
		if !ok {
			return wizard.ErrAborted
		}
	}

	for _, p := range plans {
		u.Section(p.in.Role.Title)
		if err := applyUpdate(ctx, u, p, target); err != nil {
			return err
		}
	}
	u.Line("")
	u.Box("Готово 🎉", "обновлено ролей: "+strconv.Itoa(len(plans))+" · образы "+target+"\nдля отката используйте sylph предыдущей версии: `sudo ./sylph update --no-self` (в 0.2.x флага нет, используйте `update`)", ui.Green)
	return nil
}

type rolePlan struct {
	in      installed
	env     *envfile.Env
	stage   string
	restart []compose.Service
	planErr error
}

func (p *rolePlan) cleanup() {
	if p.stage != "" {
		os.RemoveAll(p.stage)
	}
}

func (p *rolePlan) impact() string {
	switch {
	case p.planErr != nil:
		return "необходимость перезапуска неизвестна"
	case len(p.restart) == 0:
		return "перезапуск не требуется"
	}
	names := make([]string, len(p.restart))
	for i, s := range p.restart {
		names[i] = s.Name
	}
	return "перезапуск: " + strings.Join(names, ", ")
}

func (p *rolePlan) cutWarning() (string, bool) {
	if p.in.Role.Name != "node" {
		return "", false
	}
	if p.planErr != nil {
		return "нода " + p.in.Dir + ": не удалось заранее определить, будут ли перезапущены Caddy и xray; при перезапуске подключения клиентов прервутся на несколько секунд", true
	}
	var hit []string
	for _, s := range p.restart {
		if s.Name == "caddy" || s.Name == "xray" {
			hit = append(hit, s.Name)
		}
	}
	if len(hit) == 0 {
		return "", false
	}
	return "нода " + p.in.Dir + ": будут пересозданы " + strings.Join(hit, " и ") + ", подключения клиентов прервутся на несколько секунд", true
}

func updatedEnv(u *ui.UI, in installed, target string) (*envfile.Env, error) {
	defaults, _, err := in.Role.Defaults()
	if err != nil {
		return nil, err
	}
	env := defaults.Clone()
	env.Merge(in.Env)
	if err := in.Role.Pin(env, target); err != nil {
		return nil, err
	}
	if errs := in.Role.Validate(env); len(errs) > 0 {
		for _, e := range errs {
			u.Fail(e.Error())
		}
		return nil, shown(fmt.Errorf("в новой версии роли %s появились обязательные настройки: запустите `sudo ./sylph install %s --dir %s`", in.Role.Name, in.Role.Name, in.Dir))
	}
	return env, nil
}

func prepareUpdate(ctx context.Context, u *ui.UI, in installed, target string) (*rolePlan, error) {
	env, err := updatedEnv(u, in, target)
	if err != nil {
		return nil, err
	}
	p := &rolePlan{in: in, env: env}
	_, err = u.Step(ctx, in.Role.Name, func(ctx context.Context, set func(string)) (string, error) {
		stage, files, err := stageRole(in, env.Clone())
		p.stage = stage
		if err != nil {
			return "", err
		}
		set("загрузка образов " + target)
		if err := compose.PullFiles(ctx, files); err != nil {
			return "", err
		}
		set("определение перезапусков")
		plan, err := compose.Plan(ctx, files)
		if err != nil {
			p.planErr = err
			return "", ui.WarnError{Msg: "образы загружены; не удалось заранее определить перезапуски: " + err.Error()}
		}
		for _, s := range plan {
			if !s.OneShot {
				p.restart = append(p.restart, s)
			}
		}
		return "образы загружены · " + p.impact(), nil
	})
	if err != nil && !ui.IsWarn(err) {
		return p, shown(err)
	}
	return p, nil
}

func stageRole(in installed, env *envfile.Env) (string, compose.Files, error) {
	if old, err := filepath.Glob(filepath.Join(in.Dir, ".sylph-stage-*")); err == nil {
		for _, dir := range old {
			os.RemoveAll(dir)
		}
	}
	stage, err := os.MkdirTemp(in.Dir, ".sylph-stage-")
	if err != nil {
		return "", compose.Files{}, err
	}
	if _, err := writeDeployFiles(stage, in.Role); err != nil {
		return stage, compose.Files{}, err
	}
	if err := prepareEgressIn(in.Dir, stage, in.Role, env); err != nil {
		return stage, compose.Files{}, err
	}
	if err := writeEnv(stage, in.Role, env); err != nil {
		return stage, compose.Files{}, err
	}
	files := compose.Files{ProjectDir: in.Dir, EnvFile: filepath.Join(stage, ".env")}
	for _, name := range composeFiles(in.Dir, env) {
		switch {
		case filepath.IsAbs(name):
			files.Files = append(files.Files, name)
		case exists(filepath.Join(stage, name)):
			files.Files = append(files.Files, filepath.Join(stage, name))
		default:
			files.Files = append(files.Files, filepath.Join(in.Dir, name))
		}
	}
	return stage, files, nil
}

func composeFiles(dir string, env *envfile.Env) []string {
	if v := env.Get(keyComposeFile); v != "" {
		return strings.Split(v, ":")
	}
	files := []string{"compose.yml"}
	if exists(filepath.Join(dir, operatorOverride)) {
		files = append(files, operatorOverride)
	}
	return files
}

func applyUpdate(ctx context.Context, u *ui.UI, p *rolePlan, target string) error {
	in, env := p.in, p.env
	if _, err := u.Step(ctx, "Файлы роли", func(ctx context.Context, set func(string)) (string, error) {
		if err := prepareEgress(in.Dir, in.Role, env); err != nil {
			return "", err
		}
		if err := writeEnv(in.Dir, in.Role, env); err != nil {
			return "", err
		}
		backups, err := writeDeployFiles(in.Dir, in.Role)
		if err != nil {
			return "", err
		}
		if len(backups) > 0 {
			return "резервные копии: " + strings.Join(backups, ", "), nil
		}
		return "compose без изменений", nil
	}); err != nil {
		return shown(err)
	}
	if _, err := u.Step(ctx, "Образы", func(ctx context.Context, set func(string)) (string, error) {
		set("загрузка " + target)
		return "загружены", compose.Pull(ctx, in.Dir)
	}); err != nil {
		return shown(err)
	}
	if _, err := u.Step(ctx, "Запуск", func(ctx context.Context, set func(string)) (string, error) {
		return "docker compose up -d · " + p.impact(), compose.Up(ctx, in.Dir)
	}); err != nil {
		return shown(err)
	}
	return waitHealthy(ctx, u, in.Dir)
}

func pinnedCaddy(r *role.Role) string {
	tmpl, _, err := r.Template()
	if err != nil {
		return ""
	}
	return tmpl.Get(role.KeyCaddyVersion)
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
