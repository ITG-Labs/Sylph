package compose

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Project struct {
	Name   string
	Dir    string
	Status string
}

type Service struct {
	Name    string
	OneShot bool
}

type Container struct {
	ID           string
	Service      string
	State        string
	Health       string
	ExitCode     int
	Image        string
	StartedAt    time.Time
	RestartCount int
}

var Exec = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			return nil, fmt.Errorf("docker %s: %w", args[0], err)
		}
		return stdout.Bytes(), fmt.Errorf("docker %s: %s", strings.Join(firstArgs(args), " "), summarize(msg))
	}
	return stdout.Bytes(), nil
}

func firstArgs(args []string) []string {
	if len(args) > 2 {
		return args[:2]
	}
	return args
}

func summarize(s string) string {
	seen := map[string]bool{}
	var lines, errs []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
		if strings.Contains(strings.ToLower(line), "error") {
			errs = append(errs, line)
		}
	}
	if len(errs) > 0 {
		lines = errs
	}
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	return strings.Join(lines, "\n")
}

func DockerInfo(ctx context.Context) (string, string, error) {
	out, err := Exec(ctx, "", "version", "--format", "{{.Server.Version}}|{{.Server.Arch}}")
	if err != nil {
		return "", "", err
	}
	version, arch, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	return version, arch, nil
}

func Version(ctx context.Context) (string, error) {
	out, err := Exec(ctx, "", "compose", "version", "--short")
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "v"), nil
}

func AtLeast(version string, min string) bool {
	a, b := parseVersion(version), parseVersion(min)
	for i := range 3 {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}

func parseVersion(v string) [3]int {
	var out [3]int
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	for i, part := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

func Ls(ctx context.Context) ([]Project, error) {
	out, err := Exec(ctx, "", "compose", "ls", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	return parseLs(out)
}

func parseLs(out []byte) ([]Project, error) {
	var raw []struct {
		Name        string
		Status      string
		ConfigFiles string
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("docker compose ls: %w", err)
	}
	var projects []Project
	for _, r := range raw {
		first, _, _ := strings.Cut(r.ConfigFiles, ",")
		if first == "" {
			continue
		}
		projects = append(projects, Project{Name: r.Name, Dir: filepath.Dir(first), Status: r.Status})
	}
	return projects, nil
}

func Services(ctx context.Context, dir string) ([]Service, error) {
	out, err := Exec(ctx, dir, "compose", "config", "--format", "json")
	if err != nil {
		return nil, err
	}
	return parseConfig(out)
}

func parseConfig(out []byte) ([]Service, error) {
	_, services, err := parseProject(out)
	return services, err
}

func parseProject(out []byte) (string, []Service, error) {
	var cfg struct {
		Name     string `json:"name"`
		Services map[string]struct {
			Restart string `json:"restart"`
		} `json:"services"`
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		return "", nil, fmt.Errorf("docker compose config: %w", err)
	}
	var services []Service
	for name, s := range cfg.Services {
		services = append(services, Service{Name: name, OneShot: s.Restart == "" || s.Restart == "no"})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return cfg.Name, services, nil
}

type Files struct {
	ProjectDir string
	EnvFile    string
	Files      []string
}

func (f Files) flags() []string {
	args := []string{"--project-directory", f.ProjectDir, "--env-file", f.EnvFile}
	for _, file := range f.Files {
		args = append(args, "-f", file)
	}
	return args
}

var ExecCombined = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func PullFiles(ctx context.Context, f Files) error {
	_, err := Exec(ctx, f.ProjectDir, append(append([]string{"compose"}, f.flags()...), "pull", "--policy", "missing", "--quiet")...)
	return err
}

func Plan(ctx context.Context, f Files) ([]Service, error) {
	out, err := Exec(ctx, f.ProjectDir, append(append([]string{"compose"}, f.flags()...), "config", "--format", "json")...)
	if err != nil {
		return nil, err
	}
	name, services, err := parseProject(out)
	if err != nil {
		return nil, err
	}
	out, err = ExecCombined(ctx, f.ProjectDir, append(append([]string{"compose", "--progress", "json"}, f.flags()...), "up", "-d", "--remove-orphans", "--dry-run")...)
	plan, perr := parsePlan(out, name, services)
	if perr != nil {
		return nil, perr
	}
	if err != nil {
		return nil, fmt.Errorf("docker compose up --dry-run: %w", err)
	}
	return plan, nil
}

func parsePlan(out []byte, project string, services []Service) ([]Service, error) {
	byName := map[string]Service{}
	for _, s := range services {
		byName[s.Name] = s
	}
	seen := map[string]bool{}
	records := 0
	var plan []Service
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var ev struct {
			ID      string `json:"id"`
			Text    string `json:"text"`
			Error   bool   `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(bytes.TrimSpace(sc.Bytes()), &ev) != nil {
			continue
		}
		records++
		if ev.Error {
			return nil, fmt.Errorf("docker compose up --dry-run: %s", summarize(ev.Message))
		}
		if ev.Text != "Recreate" && ev.Text != "Create" {
			continue
		}
		rest, ok := strings.CutPrefix(ev.ID, "Container "+project+"-")
		if !ok {
			continue
		}
		i := strings.LastIndex(rest, "-")
		if i < 0 {
			continue
		}
		if _, err := strconv.Atoi(rest[i+1:]); err != nil {
			continue
		}
		s, ok := byName[rest[:i]]
		if !ok || seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		plan = append(plan, s)
	}
	if records == 0 {
		return nil, errors.New("docker compose не отдал план в JSON (нужен --progress json)")
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i].Name < plan[j].Name })
	return plan, nil
}

func Snapshot(ctx context.Context, dir string) ([]Container, error) {
	out, err := Exec(ctx, dir, "compose", "ps", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	containers, err := parsePs(out)
	if err != nil || len(containers) == 0 {
		return containers, err
	}
	args := []string{"inspect", "--format", "{{.Id}}|{{.State.StartedAt}}|{{.RestartCount}}|{{.State.ExitCode}}"}
	for _, c := range containers {
		args = append(args, c.ID)
	}
	out, err = Exec(ctx, dir, args...)
	if err != nil {
		return containers, nil
	}
	applyInspect(containers, out)
	return containers, nil
}

func parsePs(out []byte) ([]Container, error) {
	type row struct {
		ID       string
		Service  string
		State    string
		Health   string
		ExitCode int
		Image    string
	}
	var rows []row
	trimmed := bytes.TrimSpace(out)
	switch {
	case len(trimmed) == 0:
	case trimmed[0] == '[':
		if err := json.Unmarshal(trimmed, &rows); err != nil {
			return nil, fmt.Errorf("docker compose ps: %w", err)
		}
	default:
		sc := bufio.NewScanner(bytes.NewReader(trimmed))
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			var r row
			if err := json.Unmarshal(line, &r); err != nil {
				return nil, fmt.Errorf("docker compose ps: %w", err)
			}
			rows = append(rows, r)
		}
	}
	containers := make([]Container, len(rows))
	for i, r := range rows {
		containers[i] = Container{ID: r.ID, Service: r.Service, State: r.State, Health: r.Health, ExitCode: r.ExitCode, Image: r.Image}
	}
	return containers, nil
}

func applyInspect(containers []Container, out []byte) {
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 4 {
			continue
		}
		for i := range containers {
			if containers[i].ID == "" || !strings.HasPrefix(parts[0], containers[i].ID) {
				continue
			}
			containers[i].StartedAt, _ = time.Parse(time.RFC3339Nano, parts[1])
			containers[i].RestartCount, _ = strconv.Atoi(parts[2])
			if code, err := strconv.Atoi(parts[3]); err == nil {
				containers[i].ExitCode = code
			}
		}
	}
}

func Pull(ctx context.Context, dir string) error {
	_, err := Exec(ctx, dir, "compose", "pull", "--policy", "missing", "--quiet")
	return err
}

func Up(ctx context.Context, dir string) error {
	_, err := Exec(ctx, dir, "compose", "up", "-d", "--remove-orphans")
	return err
}

func Stop(ctx context.Context, dir string) error {
	_, err := Exec(ctx, dir, "compose", "stop")
	return err
}

func Restart(ctx context.Context, dir string, services ...string) error {
	_, err := Exec(ctx, dir, append([]string{"compose", "restart"}, services...)...)
	return err
}

func Logs(ctx context.Context, dir string, service string, tail int) string {
	out, err := Exec(ctx, dir, "compose", "logs", "--no-color", "--no-log-prefix", "--tail", strconv.Itoa(tail), service)
	if err != nil && len(out) == 0 {
		return err.Error()
	}
	return strings.TrimRight(string(out), "\n")
}

func ImageExists(ctx context.Context, image string) bool {
	_, err := Exec(ctx, "", "image", "inspect", "--format", "{{.Id}}", image)
	return err == nil
}

func PullImage(ctx context.Context, image string) error {
	_, err := Exec(ctx, "", "pull", "--quiet", image)
	return err
}

func Run(ctx context.Context, image string, args ...string) ([]byte, error) {
	base := []string{"run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", image}
	return Exec(ctx, "", append(base, args...)...)
}

type Status int

const (
	Waiting Status = iota
	Ready
	Failed
)

type ServiceState struct {
	Name   string
	Status Status
	Detail string
}

type Verdict struct {
	States []ServiceState
}

func (v Verdict) Done() bool {
	for _, s := range v.States {
		if s.Status != Ready {
			return false
		}
	}
	return len(v.States) > 0
}

func (v Verdict) Failure() (ServiceState, bool) {
	for _, s := range v.States {
		if s.Status == Failed {
			return s, true
		}
	}
	return ServiceState{}, false
}

func (v Verdict) Ready() int {
	n := 0
	for _, s := range v.States {
		if s.Status == Ready {
			n++
		}
	}
	return n
}

func Evaluate(services []Service, containers []Container, now time.Time, stable time.Duration) Verdict {
	byService := map[string]Container{}
	for _, c := range containers {
		byService[c.Service] = c
	}
	var v Verdict
	for _, s := range services {
		c, ok := byService[s.Name]
		st := ServiceState{Name: s.Name}
		switch {
		case !ok:
			st.Detail = "ещё не создан"
		case s.OneShot:
			st.Status, st.Detail = oneShot(c)
		default:
			st.Status, st.Detail = longRunning(c, now, stable)
		}
		v.States = append(v.States, st)
	}
	return v
}

func oneShot(c Container) (Status, string) {
	if c.State != "exited" {
		return Waiting, "работает"
	}
	if c.ExitCode != 0 {
		return Failed, fmt.Sprintf("завершился с кодом %d", c.ExitCode)
	}
	return Ready, "отработал"
}

func longRunning(c Container, now time.Time, stable time.Duration) (Status, string) {
	switch c.State {
	case "running":
		switch c.Health {
		case "healthy":
			return Ready, "healthy"
		case "unhealthy":
			return Waiting, "unhealthy"
		case "starting":
			return Waiting, "проверка здоровья"
		}
		if !c.StartedAt.IsZero() && now.Sub(c.StartedAt) >= stable {
			return Ready, "работает"
		}
		return Waiting, "запускается"
	case "restarting", "exited", "dead":
		if c.RestartCount >= 3 {
			return Failed, fmt.Sprintf("падает и перезапускается (%d раз), код %d", c.RestartCount, c.ExitCode)
		}
		if c.State == "restarting" {
			return Waiting, "перезапускается"
		}
		return Waiting, fmt.Sprintf("остановлен, код %d", c.ExitCode)
	}
	return Waiting, c.State
}

var ErrTimeout = errors.New("не дождались")

func Wait(ctx context.Context, dir string, timeout time.Duration, stable time.Duration, tick func(Verdict)) (Verdict, error) {
	services, err := Services(ctx, dir)
	if err != nil {
		return Verdict{}, err
	}
	deadline := time.Now().Add(timeout)
	var last Verdict
	for {
		containers, err := Snapshot(ctx, dir)
		if err != nil {
			return last, err
		}
		last = Evaluate(services, containers, time.Now(), stable)
		if tick != nil {
			tick(last)
		}
		if last.Done() {
			return last, nil
		}
		if _, failed := last.Failure(); failed {
			return last, nil
		}
		if time.Now().After(deadline) {
			return last, ErrTimeout
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
