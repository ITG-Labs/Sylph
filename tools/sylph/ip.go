package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/envfile"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/wizard"
)

const (
	keyEgressIPs     = "SYLPH_EGRESS_IPS"
	keyComposeFile   = "COMPOSE_FILE"
	egressFile       = "compose.egress.yml"
	operatorOverride = "compose.override.yml"
	maxEgressIPs     = 8
	minEgressDocker  = "28.1.0"
	minEgressCompose = "2.36.0"
	nodeProject      = "sylph-node"
)

var (
	probeURL   = "https://1.1.1.1/cdn-cgi/trace"
	probeTries = 6
	probeWait  = 5 * time.Second
	hostAddrs  = net.InterfaceAddrs
	mainSource = func() (string, error) {
		conn, err := net.Dial("udp4", "1.1.1.1:53")
		if err != nil {
			return "", err
		}
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
	}
	probeEgress    = probeFrom
	dockerVersions = func(ctx context.Context) (string, string, error) {
		v, _, err := compose.DockerInfo(ctx)
		if err != nil {
			return "", "", errors.New("docker не отвечает")
		}
		cv, err := compose.Version(ctx)
		if err != nil {
			return "", "", errors.New("плагин docker compose не установлен")
		}
		return v, cv, nil
	}
)

func parseEgressIPs(v string) ([]string, error) {
	var out []string
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if err := checkEgressIP(item); err != nil {
			return nil, fmt.Errorf("%s: %w", keyEgressIPs, err)
		}
		if slices.Contains(out, item) {
			return nil, fmt.Errorf("%s: %s указан дважды", keyEgressIPs, item)
		}
		out = append(out, item)
	}
	if len(out) > maxEgressIPs {
		return nil, fmt.Errorf("%s: не более %d адресов", keyEgressIPs, maxEgressIPs)
	}
	return out, nil
}

func checkEgressIP(v string) error {
	a, err := netip.ParseAddr(v)
	if err != nil || !a.Is4() || a.String() != v {
		return fmt.Errorf("%q не является IPv4-адресом", v)
	}
	if a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() {
		return fmt.Errorf("%s нельзя использовать для выхода в интернет", v)
	}
	return nil
}

func egressNetwork(ip string) string {
	return "egress-" + strings.ReplaceAll(ip, ".", "-")
}

func renderEgressCompose(ips []string) []byte {
	if len(ips) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("# Создано sylph из SYLPH_EGRESS_IPS в .env. Не изменяйте вручную, используйте sudo ./sylph ip.\n")
	b.WriteString("# Собственные изменения compose вносите в compose.override.yml: он подключается в COMPOSE_FILE раньше этого файла.\n")
	b.WriteString("services:\n  caddy:\n    networks:\n      default:\n        gw_priority: 100\n")
	pairs := make([]string, len(ips))
	for i, ip := range ips {
		iface := "egress" + strconv.Itoa(i+1)
		b.WriteString("      " + egressNetwork(ip) + ":\n        interface_name: " + iface + "\n")
		pairs[i] = ip + "=" + iface
	}
	b.WriteString("  agent:\n    environment:\n      SYLPH_EGRESS: " + strings.Join(pairs, ",") + "\n")
	b.WriteString("networks:\n")
	for _, ip := range ips {
		b.WriteString("  " + egressNetwork(ip) + ":\n    driver: bridge\n    driver_opts:\n      com.docker.network.host_ipv4: " + ip + "\n")
	}
	return []byte(b.String())
}

func prepareEgress(dir string, r *role.Role, env *envfile.Env) error {
	return prepareEgressIn(dir, dir, r, env)
}

func prepareEgressIn(dir string, out string, r *role.Role, env *envfile.Env) error {
	if r.Name != "node" {
		return nil
	}
	ips, err := parseEgressIPs(env.Get(keyEgressIPs))
	if err != nil {
		return err
	}
	cur := env.Get(keyComposeFile)
	ours := cur == "" || strings.HasSuffix(cur, ":"+egressFile)
	path := filepath.Join(out, egressFile)
	if len(ips) == 0 {
		if ours {
			env.Delete(keyComposeFile)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if !ours {
		return fmt.Errorf("%s в .env задан вручную (%s): добавьте в конец :%s или удалите переменную; sylph не изменяет её", keyComposeFile, cur, egressFile)
	}
	files := []string{"compose.yml"}
	if exists(filepath.Join(dir, operatorOverride)) {
		files = append(files, operatorOverride)
	}
	env.Set(keyComposeFile, strings.Join(append(files, egressFile), ":"))
	data := renderEgressCompose(ips)
	if sameFile(path, data) {
		return nil
	}
	return writeFileAtomic(path, data, 0o644)
}

func probeFrom(ctx context.Context, ip string) (string, error) {
	dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(ip)}, Timeout: 5 * time.Second}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "ip="); ok {
			return v, nil
		}
	}
	return "", errors.New("сервис проверки не вернул адрес")
}

func hostHas(ip string) (bool, error) {
	addrs, err := hostAddrs()
	if err != nil {
		return false, err
	}
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil && p.Addr().String() == ip {
			return true, nil
		}
	}
	return false, nil
}

func cmdIP(ctx context.Context, u *ui.UI, args []string) error {
	fs := newFlags(u, "ip", "sudo ./sylph ip [list | add <IPv4> | remove <IPv4>] [флаги]")
	dir := fs.String("dir", "", "каталог роли ноды")
	yes := fs.Bool("yes", false, "не запрашивать подтверждение")
	noCheck := fs.Bool("no-check", false, "не проверять доступ в интернет с адреса")
	pos, err := parseWithArgs(fs, args)
	if err != nil {
		return err
	}
	sub := "list"
	if len(pos) > 0 {
		sub = pos[0]
	}
	want := 2
	if sub == "list" {
		want = 1
	}
	if sub != "list" && sub != "add" && sub != "remove" {
		return fmt.Errorf("неизвестная подкоманда %q: допустимы list, add и remove", sub)
	}
	if len(pos) > want || (want == 2 && len(pos) < 2) {
		return errors.New("использование: sudo ./sylph ip list | add <IPv4> | remove <IPv4>")
	}
	in, err := pickRole(ctx, u, []string{"node"}, *dir)
	if err != nil {
		return err
	}
	ips, err := parseEgressIPs(in.Env.Get(keyEgressIPs))
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		return ipList(u, ips)
	case "add":
		return ipAdd(ctx, u, in, ips, pos[1], *yes, *noCheck)
	}
	return ipRemove(ctx, u, in, ips, pos[1], *yes)
}

func ipList(u *ui.UI, ips []string) error {
	if len(ips) == 0 {
		u.Info("дополнительных IP у ноды нет; для добавления используйте sudo ./sylph ip add <IPv4>")
		return nil
	}
	rows := make([][2]string, 0, len(ips))
	for _, ip := range ips {
		mark := ui.Colored(ui.Green, "на интерфейсе сервера")
		if ok, err := hostHas(ip); err != nil || !ok {
			mark = ui.Colored(ui.Yellow, "отсутствует на интерфейсах сервера, выход с него не работает")
		}
		rows = append(rows, [2]string{ip, mark})
	}
	u.Box("Дополнительные IP ноды", ui.KV(rows)+"\n\n"+ui.Dimmed("закрепление адресов за пользователями отображается в панели, на странице ноды"), ui.Accent)
	return nil
}

func egressChecks(ctx context.Context, u *ui.UI, ip string, noCheck bool) error {
	if _, err := u.Step(ctx, "Docker", func(ctx context.Context, set func(string)) (string, error) {
		v, cv, err := dockerVersions(ctx)
		if err != nil {
			return "", err
		}
		if !compose.AtLeast(v, minEgressDocker) {
			return "", fmt.Errorf("версия docker %s устарела: для дополнительных IP требуется %s или новее (приоритет шлюза и имя интерфейса сети)", v, minEgressDocker)
		}
		if !compose.AtLeast(cv, minEgressCompose) {
			return "", fmt.Errorf("версия compose %s устарела: для дополнительных IP требуется %s или новее", cv, minEgressCompose)
		}
		return v + ", compose " + cv, nil
	}); err != nil {
		return err
	}
	if _, err := u.Step(ctx, "Адрес на сервере", func(ctx context.Context, set func(string)) (string, error) {
		ok, err := hostHas(ip)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("адрес %s отсутствует на интерфейсах сервера: сначала настройте его (netplan, /etc/network/interfaces или панель хостинг-провайдера)", ip)
		}
		if main, err := mainSource(); err == nil && main == ip {
			return "", fmt.Errorf("%s — основной адрес сервера, через него уже идёт весь трафик", ip)
		}
		return "настроен", nil
	}); err != nil {
		return err
	}
	if noCheck {
		u.Warn("доступ в интернет с " + ip + " не проверялся (--no-check)")
		return nil
	}
	_, err := u.Step(ctx, "Выход в интернет", func(ctx context.Context, set func(string)) (string, error) {
		var last error
		for i := 1; i <= probeTries; i++ {
			set(fmt.Sprintf("попытка %d из %d", i, probeTries))
			seen, err := probeEgress(ctx, ip)
			switch {
			case err == nil && seen == ip:
				return "внешний адрес: " + ip, nil
			case err == nil:
				return "", fmt.Errorf("внешний адрес %s вместо %s: хостинг-провайдер подменяет адрес", seen, ip)
			}
			last = err
			if i < probeTries {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(probeWait):
				}
			}
		}
		return "", fmt.Errorf("с %s нет доступа в интернет (%v): адрес ещё не активирован провайдером или привязан к другому серверу; проверьте настройки и повторите или используйте --no-check", ip, last)
	})
	return err
}

func ipAdd(ctx context.Context, u *ui.UI, in installed, ips []string, ip string, yes, noCheck bool) error {
	if err := checkEgressIP(ip); err != nil {
		return err
	}
	if slices.Contains(ips, ip) {
		u.OK(ip + " уже добавлен")
		return nil
	}
	if len(ips) >= maxEgressIPs {
		return fmt.Errorf("у ноды уже %d дополнительных адресов, это максимум", maxEgressIPs)
	}
	u.Section("Проверки")
	if err := egressChecks(ctx, u, ip, noCheck); err != nil {
		return shown(err)
	}
	if err := confirmRecreate(u, "Добавить "+ip+"?", yes); err != nil {
		return err
	}
	if err := applyEgress(ctx, u, in, append(slices.Clone(ips), ip)); err != nil {
		return err
	}
	u.Line("")
	u.Box("Готово 🎉", strings.Join([]string{
		ip + " подключён к ноде. Примерно через полминуты он появится в панели на странице ноды,",
		"где его можно закрепить за пользователем с тарифом, включающим выделенный IP.",
		"",
		ui.Colored(ui.Yellow, "Порт ноды доступен и на "+ip+". ufw и цепочка INPUT его не закрывают, так как Docker публикует"),
		ui.Colored(ui.Yellow, "порты в обход них. Если входящие подключения на этот адрес не нужны, закройте их правилом в цепочке DOCKER-USER."),
	}, "\n"), ui.Green)
	return nil
}

func ipRemove(ctx context.Context, u *ui.UI, in installed, ips []string, ip string, yes bool) error {
	if !slices.Contains(ips, ip) {
		return fmt.Errorf("у ноды нет адреса %s (список: sudo ./sylph ip list)", ip)
	}
	u.Warn("если адрес закреплён за пользователем в панели, его трафик пойдёт через общий адрес ноды")
	if err := confirmRecreate(u, "Убрать "+ip+"?", yes); err != nil {
		return err
	}
	if err := applyEgress(ctx, u, in, slices.DeleteFunc(slices.Clone(ips), func(v string) bool { return v == ip })); err != nil {
		return err
	}
	_, _ = compose.Exec(ctx, "", "network", "rm", nodeProject+"_"+egressNetwork(ip))
	u.OK(ip + " отключён от ноды; адрес остался на интерфейсе сервера, удалите его вручную при необходимости")
	return nil
}

func confirmRecreate(u *ui.UI, title string, yes bool) error {
	if yes {
		return nil
	}
	if !u.TTY {
		return errors.New("без терминала изменение адресов выполняется только с флагом --yes")
	}
	ok, err := wizardFor(u).Confirm(title, "контейнеры ноды будут пересозданы, подключения клиентов прервутся на несколько секунд", true)
	if err != nil {
		return err
	}
	if !ok {
		return wizard.ErrAborted
	}
	return nil
}

type savedFile struct {
	path string
	data []byte
	mode os.FileMode
	had  bool
}

func saveFiles(dir string, names ...string) []savedFile {
	out := make([]savedFile, 0, len(names))
	for _, n := range names {
		p := filepath.Join(dir, n)
		data, err := os.ReadFile(p)
		f := savedFile{path: p, data: data, had: err == nil, mode: 0o600}
		if st, err := os.Stat(p); err == nil {
			f.mode = st.Mode().Perm()
		}
		out = append(out, f)
	}
	return out
}

func restoreFiles(files []savedFile) error {
	var errs []error
	for _, f := range files {
		if f.had {
			errs = append(errs, writeFileAtomic(f.path, f.data, f.mode))
			continue
		}
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func applyEgress(ctx context.Context, u *ui.UI, in installed, ips []string) error {
	env := in.Env.Clone()
	env.Set(keyEgressIPs, strings.Join(ips, ","))
	saved := saveFiles(in.Dir, ".env", egressFile)
	if _, err := u.Step(ctx, "Файлы роли", func(ctx context.Context, set func(string)) (string, error) {
		if err := prepareEgress(in.Dir, in.Role, env); err != nil {
			return "", err
		}
		if err := writeEnv(in.Dir, in.Role, env); err != nil {
			return "", err
		}
		if len(ips) == 0 {
			return ".env, " + egressFile + " удалён", nil
		}
		return ".env, " + egressFile, nil
	}); err != nil {
		return shown(errors.Join(err, restoreFiles(saved)))
	}
	_, err := u.Step(ctx, "Запуск", func(ctx context.Context, set func(string)) (string, error) {
		return "docker compose up -d", compose.Up(ctx, in.Dir)
	})
	if err == nil {
		err = waitHealthy(ctx, u, in.Dir)
	}
	if err == nil {
		return nil
	}
	if rerr := restoreFiles(saved); rerr != nil {
		u.Fail("не удалось восстановить прежние .env и " + egressFile + ": " + rerr.Error())
		return shown(err)
	}
	u.Warn("восстановлены прежние .env и " + egressFile + ", роль запускается в исходной конфигурации")
	if uerr := compose.Up(context.WithoutCancel(ctx), in.Dir); uerr != nil {
		u.Fail("роль не запустилась и с прежними файлами: " + uerr.Error())
	}
	return shown(err)
}
