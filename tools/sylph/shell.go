package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/castore"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/compose"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
)

type shellCommand struct {
	name   string
	args   string
	desc   string
	laptop bool
}

var shellCommands = []shellCommand{
	{"install", "[роль]", "поставить или перенастроить роль", false},
	{"update", "", "обновить роли машины до версии этого бинаря", false},
	{"status", "", "подробно о ролях машины", false},
	{"start", "[роль]", "запустить роль и дождаться здоровья", false},
	{"stop", "[роль]", "остановить роль", false},
	{"bundle", "<роль>", "пакет сертов роли — на ноуте рядом с CA", true},
	{"certs", "", "серты CA и их сроки", true},
	{"revoke", "<имя>", "отозвать серт", true},
	{"clear", "", "очистить вывод", false},
	{"help", "", "что тут можно", false},
	{"exit", "", "выйти", false},
}

const headerEvery = 3 * time.Second

type (
	printMsg      string
	stepStartMsg  string
	stepDetailMsg string
	stepDoneMsg   string
	clipboardMsg  string
	cmdDoneMsg    struct{}
	headerTickMsg struct{}
	formMsg       struct {
		form *huh.Form
		done chan error
	}
	headerMsg struct {
		lines []string
		roles []string
	}
)

type shellSink struct {
	p *tea.Program
}

func (s *shellSink) Print(str string) {
	s.p.Send(printMsg(str))
}

func (s *shellSink) Step(ctx context.Context, title string, fn ui.StepFunc) (string, error) {
	s.p.Send(stepStartMsg(title))
	detail, err := fn(ctx, func(d string) { s.p.Send(stepDetailMsg(d)) })
	if ctx.Err() != nil {
		err = context.Canceled
	}
	s.p.Send(stepDoneMsg(ui.StepLine(title, detail, err)))
	return detail, err
}

func (s *shellSink) Clipboard(text string) {
	s.p.Send(clipboardMsg(text))
}

func (s *shellSink) Form(f *huh.Form) error {
	done := make(chan error, 1)
	s.p.Send(formMsg{form: f, done: done})
	return <-done
}

type suggestion struct {
	value string
	label string
	desc  string
	more  bool
}

type liveStep struct {
	title   string
	detail  string
	started time.Time
}

type shell struct {
	ctx        context.Context
	u          *ui.UI
	sink       *shellSink
	root       bool
	host       string
	width      int
	height     int
	header     []string
	installed  []string
	log        []string
	vp         viewport.Model
	follow     bool
	input      textinput.Model
	history    []string
	histPos    int
	sel        int
	hidePal    bool
	busy       bool
	running    string
	cancel     context.CancelFunc
	step       *liveStep
	spin       spinner.Model
	ticking    bool
	form       *huh.Form
	formDone   chan error
	bg         tea.Msg
	refreshing bool
	quitting   bool
}

func runShell(ctx context.Context, term *ui.UI) int {
	host, err := os.Hostname()
	if err != nil {
		host = "?"
	}
	in := textinput.New()
	in.Prompt = ui.Colored(ui.Accent, "› ")
	in.Placeholder = "набери / — команды"
	m := &shell{
		ctx:    ctx,
		root:   os.Geteuid() == 0,
		host:   host,
		follow: true,
		input:  in,
		spin:   ui.Spinner(),
		vp:     viewport.New(),
	}
	m.vp.SoftWrap = true
	m.sink = &shellSink{}
	m.u = &ui.UI{Out: term.Out, TTY: true, Dark: term.Dark, Sink: m.sink}
	m.log = []string{
		ui.Dimmed("  набери / — список команд: /install, /status, /update … · PgUp/PgDn — прокрутка"),
	}
	if !m.root {
		m.log = append(m.log, ui.Dimmed("  без sudo доступны серты (/bundle, /certs, /revoke); роли машины — sudo ./sylph"))
	}
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithoutSignalHandler())
	m.sink.p = p
	_, err = p.Run()
	if m.cancel != nil {
		m.cancel()
	}
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, context.Canceled) {
		term.Fail(err.Error())
		return 1
	}
	return 0
}

func (m *shell) Init() tea.Cmd {
	return tea.Batch(m.input.Focus(), m.refreshHeader(), tickHeader(), tea.RequestBackgroundColor)
}

func tickHeader() tea.Cmd {
	return tea.Tick(headerEvery, func(time.Time) tea.Msg { return headerTickMsg{} })
}

func (m *shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.layout()
	return m, cmd
}

func (m *shell) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.form != nil {
			return m.toForm(m.formSize())
		}
		return nil
	case tea.BackgroundColorMsg:
		m.bg = msg
		if m.form != nil {
			return m.toForm(msg)
		}
		return nil
	case headerTickMsg:
		return tea.Batch(m.refreshHeader(), tickHeader())
	case headerMsg:
		m.header, m.installed, m.refreshing = msg.lines, msg.roles, false
		return nil
	case printMsg:
		m.log = append(m.log, string(msg))
		return nil
	case stepStartMsg:
		m.step = &liveStep{title: string(msg), started: time.Now()}
		return m.startSpinner()
	case stepDetailMsg:
		if m.step != nil {
			m.step.detail = string(msg)
		}
		return nil
	case stepDoneMsg:
		m.step = nil
		m.log = append(m.log, string(msg))
		return nil
	case clipboardMsg:
		return tea.SetClipboard(string(msg))
	case formMsg:
		m.form, m.formDone = msg.form, msg.done
		cmds := []tea.Cmd{m.form.Init(), m.toForm(m.formSize())}
		if m.bg != nil {
			cmds = append(cmds, m.toForm(m.bg))
		}
		return tea.Batch(cmds...)
	case cmdDoneMsg:
		m.busy, m.running, m.cancel, m.step = false, "", nil, nil
		return tea.Batch(m.input.Focus(), m.refreshHeader())
	case spinner.TickMsg:
		if !m.busy {
			m.ticking = false
			return nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return cmd
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.PasteMsg:
		if m.form != nil {
			return m.toForm(msg)
		}
		if !m.busy {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.hidePal = false
			return cmd
		}
		return nil
	}
	if m.form != nil {
		return m.toForm(msg)
	}
	return nil
}

func (m *shell) formSize() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: max(m.width-2, 20), Height: max(m.height-11, 5)}
}

func (m *shell) toForm(msg tea.Msg) tea.Cmd {
	model, cmd := m.form.Update(msg)
	if f, ok := model.(*huh.Form); ok {
		m.form = f
	}
	switch m.form.State {
	case huh.StateCompleted:
		m.formDone <- nil
		m.form = nil
	case huh.StateAborted:
		m.formDone <- huh.ErrUserAborted
		m.form = nil
	}
	return cmd
}

func (m *shell) startSpinner() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return m.spin.Tick
}

func (m *shell) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch k {
	case "pgup":
		m.vp.PageUp()
		m.follow = false
		return nil
	case "pgdown":
		m.vp.PageDown()
		m.follow = m.vp.AtBottom()
		return nil
	}
	if m.form != nil {
		return m.toForm(msg)
	}
	if m.busy {
		if k == "ctrl+c" && m.cancel != nil {
			m.cancel()
			m.log = append(m.log, ui.Dimmed("  отменяю…"))
		}
		return nil
	}
	sugg := m.suggestions()
	switch k {
	case "ctrl+c":
		if m.input.Value() != "" {
			m.input.Reset()
			return nil
		}
		m.quitting = true
		return tea.Quit
	case "ctrl+d":
		m.quitting = true
		return tea.Quit
	case "esc":
		m.hidePal = true
		return nil
	case "up":
		if len(sugg) > 0 {
			m.sel = (m.sel + len(sugg) - 1) % len(sugg)
			return nil
		}
		m.historyStep(-1)
		return nil
	case "down":
		if len(sugg) > 0 {
			m.sel = (m.sel + 1) % len(sugg)
			return nil
		}
		m.historyStep(1)
		return nil
	case "tab":
		if len(sugg) > 0 {
			m.complete(sugg[m.sel])
		}
		return nil
	case "enter":
		line := strings.TrimSpace(m.input.Value())
		if len(sugg) > 0 && sugg[m.sel].value != line {
			s := sugg[m.sel]
			if s.more {
				m.complete(s)
				return nil
			}
			line = s.value
		}
		return m.execute(line)
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.sel, m.hidePal = 0, false
	}
	return cmd
}

func (m *shell) complete(s suggestion) {
	v := s.value
	if s.more {
		v += " "
	}
	m.input.SetValue(v)
	m.input.CursorEnd()
	m.sel, m.hidePal = 0, false
}

func (m *shell) historyStep(d int) {
	if len(m.history) == 0 {
		return
	}
	m.histPos = min(max(m.histPos+d, 0), len(m.history))
	if m.histPos == len(m.history) {
		m.input.SetValue("")
		return
	}
	m.input.SetValue(m.history[m.histPos])
	m.input.CursorEnd()
}

func (m *shell) suggestions() []suggestion {
	if m.hidePal || m.busy {
		return nil
	}
	v := m.input.Value()
	if !strings.HasPrefix(v, "/") {
		return nil
	}
	name, rest, hasArg := strings.Cut(v[1:], " ")
	var out []suggestion
	if !hasArg {
		for _, c := range m.commands() {
			if strings.HasPrefix(c.name, name) {
				label := "/" + c.name
				if c.args != "" {
					label += " " + c.args
				}
				out = append(out, suggestion{value: "/" + c.name, label: label, desc: c.desc, more: strings.HasPrefix(c.args, "<")})
			}
		}
		return out
	}
	if strings.Contains(rest, " ") {
		return nil
	}
	var names []string
	switch name {
	case "install", "bundle":
		names = role.Names()
	case "start", "stop":
		names = m.installed
	}
	for _, n := range names {
		if !strings.HasPrefix(n, rest) {
			continue
		}
		desc := ""
		if r, err := role.Get(n); err == nil {
			_, desc, _ = strings.Cut(r.Title, " — ")
		}
		out = append(out, suggestion{value: "/" + name + " " + n, label: n, desc: desc, more: name == "bundle" && n == "node"})
	}
	return out
}

func (m *shell) execute(line string) tea.Cmd {
	m.input.Reset()
	m.sel, m.hidePal = 0, false
	if line == "" {
		return nil
	}
	if len(m.history) == 0 || m.history[len(m.history)-1] != line {
		m.history = append(m.history, line)
	}
	m.histPos = len(m.history)
	m.follow = true
	m.log = append(m.log, "", ui.Colored(ui.Accent, "› ")+ui.Bold(line))
	args, err := splitArgs(strings.TrimPrefix(line, "/"))
	if err != nil {
		m.log = append(m.log, ui.StepLine(line, "", err))
		return nil
	}
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "exit", "quit", "q":
		m.quitting = true
		return tea.Quit
	case "clear":
		m.log = nil
		return nil
	case "help":
		m.log = append(m.log, helpLines(m.commands(), m.root)...)
		return nil
	case "bundle", "certs", "revoke":
		if m.root {
			m.log = append(m.log, ui.StepLine(line, "", errors.New("серты — только на ноуте и без sudo: ключ CA не должен жить на машине роли")))
			return nil
		}
	}
	switch args[0] {
	case "bundle":
		args = append([]string{"certs", "bundle"}, args[1:]...)
	case "revoke":
		args = append([]string{"certs", "revoke"}, args[1:]...)
	case "certs":
		if len(args) == 1 || strings.HasPrefix(args[1], "-") {
			args = append([]string{"certs", "list"}, args[1:]...)
		}
	case "install", "update", "status", "start", "stop", "version":
	default:
		m.log = append(m.log, ui.StepLine(line, "", fmt.Errorf("нет такой команды — набери /help")))
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.busy, m.running, m.cancel = true, line, cancel
	m.input.Blur()
	go func() {
		dispatch(ctx, m.u, args)
		cancel()
		m.sink.p.Send(cmdDoneMsg{})
	}()
	return m.startSpinner()
}

func (m *shell) commands() []shellCommand {
	var out []shellCommand
	for _, c := range shellCommands {
		if m.root && c.laptop {
			continue
		}
		out = append(out, c)
	}
	return out
}

func helpLines(commands []shellCommand, root bool) []string {
	lines := []string{ui.Bold("  Команды")}
	for _, c := range commands {
		label := "/" + c.name
		if c.args != "" {
			label += " " + c.args
		}
		lines = append(lines, fmt.Sprintf("  %-18s %s", label, ui.Dimmed(c.desc)))
	}
	if root {
		lines = append(lines, ui.Dimmed("  серты (/bundle, /certs, /revoke) — на ноуте: ./sylph без sudo"))
	}
	return append(lines,
		ui.Dimmed("  флаги — как у отдельных команд: /install panel --dir /srv/panel, /bundle node --name node-de-1"),
		ui.Dimmed("  Tab — дополнить, ↑↓ — выбор и история, Esc — спрятать подсказки, Ctrl+C — отменить или выйти"),
	)
}

func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	started := false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote, started = r, true
		case r == ' ' || r == '\t':
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("не закрыта кавычка")
	}
	if started {
		args = append(args, cur.String())
	}
	return args, nil
}

func (m *shell) refreshHeader() tea.Cmd {
	if m.refreshing {
		return nil
	}
	m.refreshing = true
	ctx, root := m.ctx, m.root
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if !root {
			return headerMsg{lines: caHeader(defaultCADir(), time.Now())}
		}
		return machineHeader(ctx, time.Now())
	}
}

func machineHeader(ctx context.Context, now time.Time) headerMsg {
	if _, _, err := compose.DockerInfo(ctx); err != nil {
		return headerMsg{lines: []string{ui.Colored(ui.Red, "Docker не отвечает")}}
	}
	roles, err := findRoles(ctx, "")
	if err != nil {
		return headerMsg{lines: []string{ui.Colored(ui.Red, err.Error())}}
	}
	if len(roles) == 0 {
		return headerMsg{lines: []string{ui.Dimmed("ролей на машине нет — /install")}}
	}
	var msg headerMsg
	for _, in := range roles {
		msg.roles = append(msg.roles, in.Role.Name)
		msg.lines = append(msg.lines, roleLine(ctx, in, now))
	}
	return msg
}

func roleLine(ctx context.Context, in installed, now time.Time) string {
	name := fmt.Sprintf("%-8s", in.Role.Name)
	services, err := compose.Services(ctx, in.Dir)
	if err != nil {
		return name + ui.Colored(ui.Red, "✗ compose: "+err.Error())
	}
	containers, err := compose.Snapshot(ctx, in.Dir)
	if err != nil {
		return name + ui.Colored(ui.Red, "✗ docker: "+err.Error())
	}
	v := compose.Evaluate(services, containers, now, 0)
	count := fmt.Sprintf("%d/%d", v.Ready(), len(v.States))
	version := ui.Dimmed(" · " + in.Env.Get(role.KeyVersion))
	running := 0
	for _, c := range containers {
		if c.State == "running" || c.State == "restarting" {
			running++
		}
	}
	if running == 0 {
		return name + ui.Dimmed("■ остановлена") + version + ui.Dimmed(" · /start "+in.Role.Name)
	}
	if s, failed := v.Failure(); failed {
		return name + ui.Colored(ui.Red, "✗ "+count) + version + ui.Dimmed(" · ") + ui.Colored(ui.Red, s.Name+": "+s.Detail)
	}
	if !v.Done() {
		for _, s := range v.States {
			if s.Status != compose.Ready {
				return name + ui.Colored(ui.Yellow, "◐ "+count) + version + ui.Dimmed(" · "+s.Name+": "+s.Detail)
			}
		}
	}
	line := name + ui.Colored(ui.Green, "● "+count) + version
	if until, ok := earliestCert(in.Dir); ok {
		line += ui.Dimmed(" · серт до ") + certColor(until, now)
	}
	return line
}

func earliestCert(dir string) (time.Time, bool) {
	r, err := roleOfDir(dir)
	if err != nil {
		return time.Time{}, false
	}
	leaves, err := currentCerts(dir, r, coreNameOf(dir, r), time.Now())
	if err != nil || len(leaves) == 0 {
		return time.Time{}, false
	}
	first := leaves[0].NotAfter
	for _, l := range leaves[1:] {
		if l.NotAfter.Before(first) {
			first = l.NotAfter
		}
	}
	return first, true
}

func coreNameOf(dir string, r *role.Role) string {
	if r.Name == "core" {
		return role.DefaultCoreName
	}
	if env, err := readEnv(dir); err == nil && env.Get(role.KeyCoreName) != "" {
		return env.Get(role.KeyCoreName)
	}
	return role.DefaultCoreName
}

func certColor(until time.Time, now time.Time) string {
	text := until.Local().Format("02.01.2006")
	switch left := until.Sub(now); {
	case left < 7*24*time.Hour:
		return ui.Colored(ui.Red, text)
	case left < 30*24*time.Hour:
		return ui.Colored(ui.Yellow, text)
	}
	return ui.Dimmed(text)
}

func caHeader(dir string, now time.Time) []string {
	shown := dir
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
			shown = "~/" + rel
		}
	}
	ok, err := castore.Exists(dir)
	if err != nil {
		return []string{ui.Colored(ui.Red, "CA "+shown+": "+err.Error())}
	}
	if !ok {
		return []string{ui.Dimmed("CA " + shown + " ещё нет — его создаст первый /bundle <роль>")}
	}
	infos, err := castore.List(dir)
	if err != nil {
		return []string{ui.Colored(ui.Red, "CA "+shown+": "+err.Error())}
	}
	line := "CA " + shown + ui.Dimmed(fmt.Sprintf(" · сертов: %d", len(infos)))
	if len(infos) > 0 {
		first := infos[0].NotAfter
		for _, ci := range infos[1:] {
			if ci.NotAfter.Before(first) {
				first = ci.NotAfter
			}
		}
		line += ui.Dimmed(" · ближайший срок ") + certColor(first, now)
	}
	return []string{line}
}

func (m *shell) layout() {
	if m.width == 0 {
		return
	}
	headerH := lipgloss.Height(m.headerView())
	bottomH := lipgloss.Height(m.bottomView())
	m.vp.SetWidth(m.width)
	m.vp.SetHeight(max(m.height-headerH-bottomH, 3))
	content := strings.Join(m.log, "\n")
	if m.step != nil {
		content += "\n" + ui.LiveStep(m.spin.View(), m.step.title, m.step.detail, time.Since(m.step.started))
	}
	m.vp.SetContent(content)
	if m.follow {
		m.vp.GotoBottom()
	}
	m.input.SetWidth(max(m.width-8, 10))
}

func (m *shell) headerView() string {
	title := ui.Colored(ui.Accent, "◆ Sylph") + ui.Bold(" "+version.Version) + ui.Dimmed(" · "+m.host)
	if !m.root {
		title += ui.Dimmed(" · без sudo")
	}
	body := title
	switch {
	case m.form != nil:
		if len(m.installed) > 0 {
			body += ui.Dimmed(" · роли: " + strings.Join(m.installed, ", "))
		}
	case len(m.header) > 0:
		body += "\n" + strings.Join(m.header, "\n")
	default:
		body += "\n" + ui.Dimmed("смотрю, что на машине…")
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ui.Accent).Padding(0, 1).Width(max(m.width, 20)).Render(body)
}

func (m *shell) bottomView() string {
	if m.form != nil {
		return m.form.View()
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ui.Dim).Padding(0, 1).Width(max(m.width, 20))
	if m.busy {
		return box.Render(ui.Dimmed("выполняю "+m.running+" …")) + "\n" + ui.Dimmed("  ctrl+c — отменить · PgUp/PgDn — прокрутка")
	}
	body := m.input.View()
	sugg := m.suggestions()
	if m.sel >= len(sugg) {
		m.sel = 0
	}
	room := max(m.height-lipgloss.Height(m.headerView())-7, 2)
	from, to := 0, len(sugg)
	if len(sugg) > room {
		from = min(max(m.sel-room/2, 0), len(sugg)-room)
		to = from + room
	}
	for i := from; i < to; i++ {
		s := sugg[i]
		label := fmt.Sprintf("%-18s", s.label)
		if i == m.sel {
			body += "\n" + ui.Colored(ui.Accent, "› "+label) + " " + s.desc
		} else {
			body += "\n  " + label + " " + ui.Dimmed(s.desc)
		}
	}
	hint := "  / команды · ↑↓ история · PgUp/PgDn прокрутка · ctrl+c выход"
	if len(sugg) > 0 {
		hint = "  enter — выполнить · tab — дополнить · ↑↓ выбор · esc — спрятать"
	}
	return box.Render(body) + "\n" + ui.Dimmed(hint)
}

func (m *shell) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	if m.width == 0 {
		return tea.NewView("")
	}
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, m.headerView(), m.vp.View(), m.bottomView()))
	v.AltScreen = true
	v.WindowTitle = "sylph · " + m.host
	return v
}
