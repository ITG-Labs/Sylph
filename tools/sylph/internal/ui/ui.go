package ui

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"rsc.io/qr"
)

var (
	Accent = lipgloss.Color("#A78BFA")
	Green  = lipgloss.Color("#4ADE80")
	Yellow = lipgloss.Color("#FACC15")
	Red    = lipgloss.Color("#F87171")
	Dim    = lipgloss.Color("#8B8B9E")
	white  = lipgloss.Color("#FFFFFF")
	black  = lipgloss.Color("#000000")
)

var (
	accent = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	green  = lipgloss.NewStyle().Foreground(Green)
	yellow = lipgloss.NewStyle().Foreground(Yellow)
	red    = lipgloss.NewStyle().Foreground(Red)
	dim    = lipgloss.NewStyle().Foreground(Dim)
	bold   = lipgloss.NewStyle().Bold(true)
)

type UI struct {
	Out  io.Writer
	TTY  bool
	Dark bool
}

func New() *UI {
	tty := isTerminal(os.Stdin) && isTerminal(os.Stdout)
	u := &UI{Out: os.Stdout, TTY: tty, Dark: true}
	if tty {
		u.Dark = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	}
	return u
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func (u *UI) print(s string) {
	lipgloss.Fprintln(u.Out, s)
}

func (u *UI) Banner(title string, subtitle string) {
	logo := accent.Render("◆ Sylph") + dim.Render(" · ") + bold.Render(title)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Accent).Padding(0, 2)
	u.print(box.Render(logo + "\n" + dim.Render(subtitle)))
}

func (u *UI) Section(title string) {
	u.print("\n" + accent.Render("▸ "+title))
}

func (u *UI) OK(msg string) {
	u.print(green.Render("  ✓ ") + msg)
}

func (u *UI) Warn(msg string) {
	u.print(yellow.Render("  ! ") + msg)
}

func (u *UI) Fail(msg string) {
	u.print(red.Render("  ✗ ") + msg)
}

func (u *UI) Info(msg string) {
	u.print(dim.Render("  · ") + msg)
}

func (u *UI) Line(msg string) {
	u.print(msg)
}

func Dimmed(s string) string { return dim.Render(s) }

func Bold(s string) string { return bold.Render(s) }

func Colored(c color.Color, s string) string {
	return lipgloss.NewStyle().Foreground(c).Render(s)
}

func Box(title string, body string, c color.Color) string {
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c).Padding(0, 1)
	head := lipgloss.NewStyle().Foreground(c).Bold(true).Render(title)
	return style.Render(head + "\n" + body)
}

func (u *UI) Box(title string, body string, c color.Color) {
	u.print(Box(title, body, c))
}

func KV(rows [][2]string) string {
	width := 0
	for _, r := range rows {
		if w := utf8.RuneCountInString(r[0]); w > width {
			width = w
		}
	}
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(r[0]))
		b.WriteString(dim.Render(r[0]+pad) + "  " + r[1])
	}
	return b.String()
}

func Mask(secret string) string {
	if secret == "" {
		return dim.Render("—")
	}
	tail := secret
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	return "••••" + tail
}

func QR(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 2
	style := lipgloss.NewStyle().Foreground(black).Background(white)
	var b strings.Builder
	for y := -quiet; y < code.Size+quiet; y += 2 {
		var line strings.Builder
		for x := -quiet; x < code.Size+quiet; x++ {
			top, bottom := code.Black(x, y), code.Black(x, y+1)
			switch {
			case top && bottom:
				line.WriteString("█")
			case top:
				line.WriteString("▀")
			case bottom:
				line.WriteString("▄")
			default:
				line.WriteString(" ")
			}
		}
		b.WriteString(style.Render(line.String()))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type StepFunc func(ctx context.Context, set func(detail string)) (string, error)

type WarnError struct{ Msg string }

func (w WarnError) Error() string { return w.Msg }

func (u *UI) Step(ctx context.Context, title string, fn StepFunc) (string, error) {
	if !u.TTY {
		lipgloss.Fprintln(u.Out, dim.Render("  … ")+title)
		detail, err := fn(ctx, func(string) {})
		u.finish(title, detail, err)
		return detail, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &stepModel{title: title, spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(Accent))), cancel: cancel, started: time.Now()}
	p := tea.NewProgram(m, tea.WithoutSignalHandler())
	var detail string
	var err error
	finished := make(chan struct{})
	go func() {
		detail, err = fn(ctx, func(d string) { p.Send(detailMsg(d)) })
		close(finished)
		p.Send(doneMsg{})
	}()
	if _, runErr := p.Run(); runErr != nil {
		cancel()
	}
	<-finished
	u.finish(title, detail, err)
	return detail, err
}

func (u *UI) finish(title string, detail string, err error) {
	switch {
	case err == nil:
		line := green.Render("  ✓ ") + title
		if detail != "" {
			line += dim.Render(" · " + detail)
		}
		u.print(line)
	case isWarn(err):
		u.print(yellow.Render("  ! ") + title + dim.Render(" · ") + err.Error())
	default:
		u.print(red.Render("  ✗ ") + title + dim.Render(" · ") + red.Render(err.Error()))
	}
}

func isWarn(err error) bool {
	_, ok := err.(WarnError)
	return ok
}

func IsWarn(err error) bool { return isWarn(err) }

type detailMsg string

type doneMsg struct{}

type stepModel struct {
	title   string
	detail  string
	spin    spinner.Model
	cancel  context.CancelFunc
	started time.Time
	done    bool
}

func (m *stepModel) Init() tea.Cmd {
	return m.spin.Tick
}

func (m *stepModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case detailMsg:
		m.detail = string(msg)
	case doneMsg:
		m.done = true
		return m, tea.Quit
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.cancel()
			m.detail = "отменяю…"
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *stepModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	line := "  " + m.spin.View() + " " + m.title
	elapsed := time.Since(m.started).Round(time.Second)
	if m.detail != "" {
		line += dim.Render(" · " + m.detail)
	}
	if elapsed >= 3*time.Second {
		line += dim.Render(fmt.Sprintf(" (%s)", elapsed))
	}
	return tea.NewView(line)
}
