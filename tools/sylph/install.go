package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/tools/sylph/internal/bundle"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/envfile"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/machine"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/wizard"
)

const (
	minCompose    = "2.24.0"
	healthTimeout = 3 * time.Minute
	stableFor     = 15 * time.Second
	acmeTimeout   = 2 * time.Minute
)

type installOpts struct {
	role    string
	dir     string
	envFile string
	certs   string
	admins  []string
	yes     bool
	version string
}

func cmdInstall(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags("install", "sudo ./sylph install [роль] [флаги]")
	dir := fs.String("dir", "", "папка роли (по умолчанию ./sylph-<роль>)")
	envFile := fs.String("env", "", "файл с настройками роли: установка без вопросов")
	certs := fs.String("certs", "", "пакет сертов: файл .sylph-certs или строка sylph-certs:…")
	var admins multiFlag
	fs.Var(&admins, "admin", "имя нового админа панели (флаг можно повторять)")
	yes := fs.Bool("yes", false, "не спрашивать подтверждение; предупреждения не останавливают")
	ver := fs.String("version", "", "тег образов вместо версии бинаря")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fmt.Errorf("лишние аргументы: %s", strings.Join(pos[1:], " "))
	}
	o := installOpts{dir: *dir, envFile: *envFile, certs: *certs, admins: admins, yes: *yes, version: *ver}
	if len(pos) == 1 {
		o.role = pos[0]
	}
	return install(ctx, u, o)
}

type plan struct {
	r            *role.Role
	dir          string
	env          *envfile.Env
	prevVersion  string
	firstInstall bool
	newBundle    *bundle.Bundle
	leaves       []bundle.Leaf
	newAdmins    []string
	admins       []adminRecord
	adminNames   []string
}

func (p *plan) coreName() string {
	if p.r.Name == "core" {
		return role.DefaultCoreName
	}
	if n := p.env.Get(role.KeyCoreName); n != "" {
		return n
	}
	return role.DefaultCoreName
}

func install(ctx context.Context, u *ui.UI, o installOpts) error {
	if err := machine.Root(); err != nil {
		return err
	}
	imgVer, err := imageVersion(o.version)
	if err != nil {
		return err
	}
	interactive := u.TTY && o.envFile == ""
	if o.envFile == "" && !u.TTY {
		return errors.New("нет терминала для мастера: передай настройки через --env <файл>")
	}
	var wz wizard.Wizard
	u.Banner("установка роли", "образы "+imgVer+" · Ctrl+C — выйти")
	u.Section("Docker")
	if err := dockerChecks(ctx, u); err != nil {
		return shown(err)
	}

	p := &plan{}
	switch {
	case o.role != "":
		p.r, err = role.Get(o.role)
	case interactive:
		p.r, err = wz.ChooseRole()
	default:
		err = errors.New("укажи роль: sudo ./sylph install <роль> --env …")
	}
	if err != nil {
		return err
	}
	if p.dir, err = roleDir(o.dir, p.r); err != nil {
		return err
	}
	if err := p.load(imgVer); err != nil {
		return err
	}

	now := time.Now()
	curLeaves, curErr := currentCerts(p.dir, p.r, p.coreName(), now)
	certsUntil := ""
	if curErr == nil {
		certsUntil = bundle.Earliest(curLeaves).NotAfter.Local().Format("02.01.2006")
		p.leaves = curLeaves
	}

	bundleIn := ""
	if interactive {
		_, examples, err := p.r.Template()
		if err != nil {
			return err
		}
		exampleEnv, err := envfile.Parse(examples)
		if err != nil {
			return err
		}
		ans, err := wz.Ask(wizard.Request{
			Role:           p.r,
			Env:            p.env,
			Examples:       exampleEnv,
			FirstInstall:   p.firstInstall,
			CertsUntil:     certsUntil,
			ExistingAdmins: p.adminNames,
			CheckBundle: func(s string) error {
				_, _, err := loadBundle(s, p.r, p.coreName(), time.Now())
				return err
			},
		})
		if err != nil {
			return err
		}
		p.env, bundleIn, p.newAdmins = ans.Env, ans.Bundle, ans.NewAdmins
	} else {
		if err := p.fromFile(o, imgVer); err != nil {
			return err
		}
		bundleIn = o.certs
		if bundleIn == "" && curErr != nil {
			return errors.New("нужен пакет сертов: --certs <файл или строка sylph-certs:…>")
		}
	}
	if p.r.Admins && len(p.adminNames) == 0 && len(p.newAdmins) == 0 {
		return errors.New("у панели нет ни одного админа: добавь хотя бы одного")
	}
	if errs := p.r.Validate(p.env); len(errs) > 0 {
		u.Section("Настройки с ошибками")
		for _, e := range errs {
			u.Fail(e.Error())
		}
		return shown(errors.New("настройки с ошибками"))
	}
	if bundleIn != "" {
		if p.newBundle, p.leaves, err = loadBundle(bundleIn, p.r, p.coreName(), now); err != nil {
			return err
		}
	}

	u.Section("Проверка машины")
	warnings, err := p.checks(ctx, u, now)
	if err != nil {
		return shown(err)
	}

	u.Section("Что ставлю")
	u.Box(p.r.Title, p.summary(), ui.Accent)
	if warnings > 0 {
		u.Warn(fmt.Sprintf("предупреждений: %d — посмотри выше", warnings))
	}
	switch {
	case interactive && !o.yes:
		ok, err := wz.Confirm("Ставим?", "запишу папку роли, подниму контейнеры и дождусь здоровья", true)
		if err != nil {
			return err
		}
		if !ok {
			return wizard.ErrAborted
		}
	case !interactive && warnings > 0 && !o.yes:
		return errors.New("есть предупреждения: проверь их и запусти ещё раз с --yes")
	}

	u.Section("Установка")
	return p.apply(ctx, u, wz, interactive)
}

func (p *plan) load(imgVer string) error {
	defaults, _, err := p.r.Defaults()
	if err != nil {
		return err
	}
	p.env = defaults.Clone()
	envPath := filepath.Join(p.dir, ".env")
	p.firstInstall = !exists(envPath)
	if !p.firstInstall {
		if existing, err := roleOfDir(p.dir); err == nil && existing.Name != p.r.Name {
			return fmt.Errorf("в %s стоит роль %s, а не %s", p.dir, existing.Name, p.r.Name)
		}
		old, err := readEnv(p.dir)
		if err != nil {
			return err
		}
		p.prevVersion = old.Get(role.KeyVersion)
		p.env.Merge(old)
	}
	if p.r.Name == "core" && p.firstInstall && exists(filepath.Join(p.dir, "data", "postgres")) {
		return fmt.Errorf("в %s есть база Postgres, но нет .env: пароль базы не восстановить — верни .env", p.dir)
	}
	p.env.Set(role.KeyVersion, imgVer)
	if p.r.Name == "core" && p.env.Get(role.KeyPassword) == "" {
		pw, err := randomHex(32)
		if err != nil {
			return err
		}
		p.env.Set(role.KeyPassword, pw)
	}
	if p.r.Admins {
		if p.admins, p.adminNames, err = readAdmins(p.dir); err != nil {
			return err
		}
	}
	return nil
}

func (p *plan) fromFile(o installOpts, imgVer string) error {
	data, err := os.ReadFile(o.envFile)
	if err != nil {
		return err
	}
	file, err := envfile.Parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", o.envFile, err)
	}
	password := p.env.Get(role.KeyPassword)
	p.env.Merge(file)
	p.env.Set(role.KeyVersion, imgVer)
	if p.r.Name == "core" && !p.firstInstall && p.env.Get(role.KeyPassword) != password {
		return errors.New("POSTGRES_PASSWORD менять нельзя: база создана со старым паролем")
	}
	if p.r.Name == "core" && p.env.Get(role.KeyPassword) == "" {
		p.env.Set(role.KeyPassword, password)
	}
	if !p.r.Admins {
		if len(o.admins) > 0 {
			return fmt.Errorf("--admin бывает только у панели")
		}
		return nil
	}
	names, err := role.AdminNames(strings.Join(o.admins, ","))
	if err != nil {
		return err
	}
	for _, n := range names {
		for _, e := range p.adminNames {
			if n == e {
				return fmt.Errorf("админ %q уже есть", n)
			}
		}
	}
	p.newAdmins = names
	return nil
}

func loadBundle(input string, r *role.Role, coreName string, now time.Time) (*bundle.Bundle, []bundle.Leaf, error) {
	b, err := bundle.Load(input)
	if err != nil {
		return nil, nil, err
	}
	leaves, err := b.Verify(r.Name, coreName, now)
	if err != nil {
		return nil, nil, err
	}
	return b, leaves, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func dockerChecks(ctx context.Context, u *ui.UI) error {
	if _, err := u.Step(ctx, "Docker", func(ctx context.Context, set func(string)) (string, error) {
		v, arch, err := compose.DockerInfo(ctx)
		if err != nil {
			return "", errors.New("не отвечает: поставь Docker Engine (docs.docker.com/engine/install) и запусти его")
		}
		if arch != "amd64" && arch != "x86_64" {
			return "", fmt.Errorf("образы Sylph собраны только под amd64, а тут %s", arch)
		}
		return v, nil
	}); err != nil {
		return err
	}
	_, err := u.Step(ctx, "Docker Compose", func(ctx context.Context, set func(string)) (string, error) {
		v, err := compose.Version(ctx)
		if err != nil {
			return "", errors.New("нет плагина docker compose: поставь docker-compose-plugin")
		}
		if !compose.AtLeast(v, minCompose) {
			return "", fmt.Errorf("compose %s старый, нужен %s или новее", v, minCompose)
		}
		return v, nil
	})
	return err
}

func result(r machine.Result) (string, error) {
	switch r.Level {
	case machine.Warn:
		return "", ui.WarnError{Msg: r.Detail}
	case machine.Fail:
		return "", errors.New(r.Detail)
	}
	return r.Detail, nil
}

func (p *plan) checks(ctx context.Context, u *ui.UI, now time.Time) (int, error) {
	warnings := 0
	step := func(title string, fn func(ctx context.Context) machine.Result) error {
		_, err := u.Step(ctx, title, func(ctx context.Context, set func(string)) (string, error) {
			return result(fn(ctx))
		})
		if ui.IsWarn(err) {
			warnings++
			return nil
		}
		return err
	}
	running := projectRunning(ctx, p.dir)
	if port := p.env.Get(role.KeyPort); port != "" && !running {
		if err := step("Порт "+port, func(context.Context) machine.Result { return machine.PortFree(port) }); err != nil {
			return warnings, err
		}
	}
	if err := step("Часы", machine.TimeSync); err != nil {
		return warnings, err
	}
	if err := step("Место на диске", func(context.Context) machine.Result { return machine.DiskFree(p.dir) }); err != nil {
		return warnings, err
	}
	if p.r.DomainKey != "" {
		domain := p.env.Get(p.r.DomainKey)
		for _, res := range machine.DNS(ctx, domain, machine.LocalIPs()) {
			if err := step("DNS", func(context.Context) machine.Result { return res }); err != nil {
				return warnings, err
			}
		}
	}
	if p.r.TokenKey != "" {
		token := p.env.Get(p.r.TokenKey)
		if err := step("Токен бота", func(ctx context.Context) machine.Result { return machine.TelegramGetMe(ctx, token) }); err != nil {
			return warnings, err
		}
	}
	if err := step("Серты", func(context.Context) machine.Result { return p.certsResult(now) }); err != nil {
		return warnings, err
	}
	if p.r.Name != "core" {
		cc, err := p.clientCert()
		if err != nil {
			return warnings, err
		}
		if err := step("Ядро", func(ctx context.Context) machine.Result {
			return machine.ProbeCore(ctx, p.env.Get(role.KeyCoreURL), p.coreName(), cc)
		}); err != nil {
			return warnings, err
		}
	}
	return warnings, nil
}

func (p *plan) certsResult(now time.Time) machine.Result {
	first := bundle.Earliest(p.leaves)
	var names []string
	for _, l := range p.leaves {
		names = append(names, l.Name)
	}
	source := "текущие"
	if p.newBundle != nil {
		source = "новый пакет"
	}
	detail := fmt.Sprintf("%s: %s, до %s", source, strings.Join(names, " + "), first.NotAfter.Local().Format("02.01.2006"))
	if first.NotAfter.Sub(now) < 30*24*time.Hour {
		return machine.Result{Level: machine.Warn, Detail: detail + " — меньше 30 дней, перевыпусти"}
	}
	return machine.Result{Level: machine.OK, Detail: detail}
}

func (p *plan) clientCert() (machine.ClientCert, error) {
	files := map[string][]byte{}
	if p.newBundle != nil {
		files = p.newBundle.Files
	} else {
		for _, name := range []string{"ca.pem", "cert.pem", "key.pem"} {
			data, err := os.ReadFile(filepath.Join(p.dir, "certs", name))
			if err != nil {
				return machine.ClientCert{}, err
			}
			files[name] = data
		}
	}
	return machine.ClientCert{CA: files["ca.pem"], CertPEM: files["cert.pem"], KeyPEM: files["key.pem"]}, nil
}

func (p *plan) summary() string {
	ver := p.env.Get(role.KeyVersion)
	if p.prevVersion != "" && p.prevVersion != ver {
		ver = p.prevVersion + " → " + ver
	}
	rows := [][2]string{{"папка", p.dir}, {"образы", ver}}
	if p.firstInstall {
		rows[0][1] += ui.Dimmed(" (новая)")
	}
	defaults, _, err := p.r.Defaults()
	if err != nil {
		defaults = envfile.New()
	}
	for _, f := range p.r.Fields {
		if f.Toggle != "" && !p.r.ToggleOn(f.Toggle, p.env) {
			continue
		}
		v := p.env.Get(f.Key)
		if f.Advanced && v == defaults.Get(f.Key) {
			continue
		}
		rows = append(rows, [2]string{f.Title, display(f, v)})
	}
	for _, t := range p.r.Toggles {
		if !p.r.ToggleOn(t.Name, p.env) {
			rows = append(rows, [2]string{t.Title, "нет"})
		}
	}
	if len(p.leaves) > 0 {
		rows = append(rows, [2]string{"серты", p.certsResult(time.Now()).Detail})
	}
	if p.r.Admins {
		admins := strings.Join(p.adminNames, ", ")
		if len(p.newAdmins) > 0 {
			if admins != "" {
				admins += " + "
			}
			admins += "новые: " + strings.Join(p.newAdmins, ", ")
		}
		rows = append(rows, [2]string{"админы", admins})
	}
	return ui.KV(rows)
}

func display(f role.Field, v string) string {
	switch {
	case f.Kind == role.Secret:
		return ui.Mask(v)
	case f.Kind == role.Bool && v == "true":
		return "да"
	case f.Kind == role.Bool:
		return "нет"
	case v == "":
		return ui.Dimmed("—")
	}
	return v
}

func (p *plan) apply(ctx context.Context, u *ui.UI, wz wizard.Wizard, interactive bool) error {
	wasRunning := projectRunning(ctx, p.dir)
	if _, err := u.Step(ctx, "Папка роли", func(ctx context.Context, set func(string)) (string, error) {
		if err := os.MkdirAll(p.dir, 0o755); err != nil {
			return "", err
		}
		if err := chownToSudoUser(p.dir); err != nil {
			return "", err
		}
		backups, err := writeDeployFiles(p.dir, p.r)
		if err != nil {
			return "", err
		}
		if err := writeEnv(p.dir, p.r, p.env); err != nil {
			return "", err
		}
		if len(backups) > 0 {
			return p.dir + ", прежние: " + strings.Join(backups, ", "), nil
		}
		return p.dir, nil
	}); err != nil {
		return shown(err)
	}

	certsChanged := false
	if p.newBundle != nil {
		if _, err := u.Step(ctx, "Серты", func(ctx context.Context, set func(string)) (string, error) {
			changed, err := writeCerts(p.dir, p.newBundle)
			certsChanged = changed
			if err != nil {
				return "", err
			}
			if !changed {
				return "те же, что стояли", nil
			}
			return "разложены в certs/", nil
		}); err != nil {
			return shown(err)
		}
	}

	if _, err := u.Step(ctx, "Образы", func(ctx context.Context, set func(string)) (string, error) {
		set("тяну недостающие")
		if err := compose.Pull(ctx, p.dir); err != nil {
			return "", err
		}
		return "на месте", nil
	}); err != nil {
		return shown(err)
	}

	if len(p.newAdmins) > 0 {
		if err := p.createAdmins(ctx, u, wz, interactive); err != nil {
			return err
		}
	}

	if _, err := u.Step(ctx, "Запуск", func(ctx context.Context, set func(string)) (string, error) {
		return "docker compose up -d", compose.Up(ctx, p.dir)
	}); err != nil {
		return shown(err)
	}

	if wasRunning && (certsChanged || len(p.newAdmins) > 0) {
		if _, err := u.Step(ctx, "Перезапуск", func(ctx context.Context, set func(string)) (string, error) {
			return strings.Join(p.r.CertReaders, ", "), compose.Restart(ctx, p.dir, p.r.CertReaders...)
		}); err != nil {
			return shown(err)
		}
	}

	if err := waitHealthy(ctx, u, p.dir); err != nil {
		return err
	}

	if p.r.Name == "core" {
		if _, err := u.Step(ctx, "Ядро снаружи", func(ctx context.Context, set func(string)) (string, error) {
			cc, err := cronCert(p.dir)
			if err != nil {
				return "", err
			}
			res := machine.ProbeCore(ctx, "https://127.0.0.1:"+p.env.Get(role.KeyPort), role.DefaultCoreName, cc)
			if res.Level == machine.OK {
				return "порт " + p.env.Get(role.KeyPort) + " отвечает по mTLS", nil
			}
			return result(res)
		}); err != nil && !ui.IsWarn(err) {
			return shown(err)
		}
	}
	if p.r.DomainKey != "" {
		if _, err := u.Step(ctx, "Серт Let's Encrypt", func(ctx context.Context, set func(string)) (string, error) {
			return waitACME(ctx, p.dir, p.env.Get(p.r.DomainKey))
		}); err != nil && !ui.IsWarn(err) {
			return shown(err)
		}
	}

	u.Line("")
	u.Box("Готово 🎉 "+p.r.Title, ui.KV([][2]string{
		{"папка", p.dir},
		{"образы", p.env.Get(role.KeyVersion)},
		{"здоровье", ui.Colored(ui.Green, "все контейнеры в порядке")},
	})+"\n\n"+ui.Bold("Дальше:")+"\n"+strings.Join(p.nextSteps(), "\n"), ui.Green)
	return nil
}

func (p *plan) createAdmins(ctx context.Context, u *ui.UI, wz wizard.Wizard, interactive bool) error {
	image := "ghcr.io/itg-labs/sylph-panel:" + p.env.Get(role.KeyVersion)
	type created struct {
		name, password, uri string
	}
	var made []created
	if _, err := u.Step(ctx, "Админы панели", func(ctx context.Context, set func(string)) (string, error) {
		for _, name := range p.newAdmins {
			set(name)
			out, err := compose.Run(ctx, image, "new-admin", "--name", name, "--json")
			if err != nil {
				return "", err
			}
			var res struct {
				Record   json.RawMessage `json:"record"`
				Password string          `json:"password"`
				TOTPURI  string          `json:"totp_uri"`
			}
			if err := json.Unmarshal(out, &res); err != nil || len(res.Record) == 0 || res.Password == "" {
				return "", errors.New("new-admin в образе панели ответил не JSON — образ старше установщика?")
			}
			p.admins = append(p.admins, adminRecord(res.Record))
			made = append(made, created{name, res.Password, res.TOTPURI})
		}
		if err := writeAdmins(p.dir, p.admins); err != nil {
			return "", err
		}
		return strings.Join(p.newAdmins, ", "), nil
	}); err != nil {
		return shown(err)
	}
	for _, a := range made {
		body := ui.KV([][2]string{{"логин", a.name}, {"пароль", ui.Bold(a.password)}, {"TOTP", a.uri}})
		if qr, err := ui.QR(a.uri); err == nil {
			body += "\n\n" + qr
		}
		u.Box("Админ "+a.name+" — пароль и TOTP показываю один раз", body, ui.Yellow)
		if interactive {
			if err := wz.Pause("Сохранил пароль и добавил TOTP в приложение?", "больше их нигде не будет: в admins.json только хэш и секрет"); err != nil {
				return err
			}
		}
	}
	return nil
}

func cronCert(dir string) (machine.ClientCert, error) {
	var cc machine.ClientCert
	var err error
	if cc.CA, err = os.ReadFile(filepath.Join(dir, "certs", "ca.pem")); err != nil {
		return cc, err
	}
	if cc.CertPEM, err = os.ReadFile(filepath.Join(dir, "certs", "cron", "cert.pem")); err != nil {
		return cc, err
	}
	cc.KeyPEM, err = os.ReadFile(filepath.Join(dir, "certs", "cron", "key.pem"))
	return cc, err
}

func waitHealthy(ctx context.Context, u *ui.UI, dir string) error {
	var verdict compose.Verdict
	_, err := u.Step(ctx, "Здоровье", func(ctx context.Context, set func(string)) (string, error) {
		v, err := compose.Wait(ctx, dir, healthTimeout, stableFor, func(v compose.Verdict) {
			set(progress(v))
		})
		verdict = v
		if err != nil {
			if errors.Is(err, compose.ErrTimeout) {
				return "", fmt.Errorf("за %s не все поднялись: %s", healthTimeout, progress(v))
			}
			return "", err
		}
		if s, failed := v.Failure(); failed {
			return "", fmt.Errorf("%s: %s", s.Name, s.Detail)
		}
		return fmt.Sprintf("%d/%d контейнеров в порядке", v.Ready(), len(v.States)), nil
	})
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	bad := ""
	if s, failed := verdict.Failure(); failed {
		bad = s.Name
	} else {
		for _, s := range verdict.States {
			if s.Status != compose.Ready {
				bad = s.Name
				break
			}
		}
	}
	if bad != "" {
		u.Box("Лог "+bad+" (последние 40 строк)", compose.Logs(context.WithoutCancel(ctx), dir, bad, 40), ui.Red)
		u.Info("полный лог: cd " + dir + " && sudo docker compose logs " + bad)
	}
	return shown(err)
}

func progress(v compose.Verdict) string {
	s := fmt.Sprintf("%d/%d готовы", v.Ready(), len(v.States))
	for _, st := range v.States {
		if st.Status != compose.Ready {
			return s + " · " + st.Name + ": " + st.Detail
		}
	}
	return s
}

func waitACME(ctx context.Context, dir string, domain string) (string, error) {
	pattern := filepath.Join(dir, "data", "caddy", "certificates", "*", domain, domain+".crt")
	deadline := time.Now().Add(acmeTimeout)
	for {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		if len(matches) > 0 {
			return "есть для " + domain, nil
		}
		if time.Now().After(deadline) {
			return "", ui.WarnError{Msg: "за 2 мин не выпущен — проверь DNS и 443/tcp у провайдера; лог: docker compose logs caddy"}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (p *plan) nextSteps() []string {
	port := p.env.Get(role.KeyPort)
	addr := "<IP машины>"
	for _, ip := range machine.LocalIPs() {
		if ip.Is4() {
			addr = ip.String()
			break
		}
	}
	var steps []string
	switch p.r.Name {
	case "core":
		steps = append(steps,
			"• остальным ролям: SYLPH_CORE_URL=https://"+addr+":"+port,
			"• дальше по порядку: ноды → панель → бот и subs → монитор",
		)
	case "panel":
		steps = append(steps,
			"• вход: https://"+p.env.Get(p.r.DomainKey),
			"• «Настройки» → название подписки и ссылка поддержки",
		)
	case "bot":
		steps = append(steps, "• в кабинете ЮKassa URL уведомлений: https://"+p.env.Get(p.r.DomainKey)+"/yookassa")
	case "subs":
		steps = append(steps, "• у бота SYLPH_SUBS_URL=https://"+p.env.Get(p.r.DomainKey))
	case "node":
		cn := ""
		if len(p.leaves) > 0 {
			cn = p.leaves[0].Name
		}
		steps = append(steps,
			"• в панели создай ноду: cert_cn = "+cn+", адрес = "+addr,
			"• профили маршрутизации — после того, как нода вышла на связь",
		)
	case "monitor":
		steps = append(steps, "• каждый из получателей пишет боту алертов /start и проверяет /status")
	}
	if port != "" {
		steps = append(steps, "• "+port+"/tcp должен быть открыт у провайдера (Docker публикует порт мимо ufw/firewalld)")
	}
	return steps
}
