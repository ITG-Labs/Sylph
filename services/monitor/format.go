package monitor

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	statusLineRunes = 150
	statusListRunes = 3000
)

var apiStatus = regexp.MustCompile(`api (\d{3})`)

func FormatAlert(a Alert, now time.Time, loc *time.Location, panelURL string) string {
	p := a.Problem
	if a.Kind == KindResolved {
		return "🟢 Починилось: " + p.Text
	}
	icon := "🔴"
	if p.Level <= 1 {
		icon = "🟠"
	}
	since := p.Since
	if since == nil && !a.FirstSeen.IsZero() {
		since = &a.FirstSeen
	}
	var b strings.Builder
	b.WriteString(icon + " " + p.Text)
	if since != nil {
		b.WriteString("\nс " + clock(*since, now, loc))
	}
	if path := panelPath(p.Key); panelURL != "" && path != "" {
		b.WriteString("\n" + panelURL + path)
	}
	return b.String()
}

func clock(t, now time.Time, loc *time.Location) string {
	lt, ln := t.In(loc), now.In(loc)
	if lt.YearDay() == ln.YearDay() && lt.Year() == ln.Year() {
		return lt.Format("15:04 MST")
	}
	return lt.Format("02.01 15:04 MST")
}

func panelPath(key string) string {
	parts := strings.Split(key, ":")
	switch {
	case key == CoreUnreachableKey:
		return ""
	case parts[0] == "node" && len(parts) == 3:
		return "/nodes/" + parts[1]
	case parts[0] == "cert" && len(parts) > 1 && parts[1] == "node":
		return "/nodes"
	case parts[0] == "money":
		return "/payments"
	}
	return "/"
}

var roleNames = map[string]string{"bot": "Бот", "subs": "subs", "cron": "Крон", "panel": "Панель", "monitor": "Монитор"}

func FormatStatus(st *State, coreErr error, now time.Time, loc *time.Location) string {
	if coreErr != nil || st == nil {
		return "🔴 Ядро недоступно: монитор не может получить состояние"
	}
	var b strings.Builder
	if len(st.Problems) == 0 {
		b.WriteString("✅ Всё зелёное\n")
	} else {
		fmt.Fprintf(&b, "🔴 Проблем: %d\n", len(st.Problems))
		used := 0
		for i, p := range st.Problems {
			icon := "🔴"
			if p.Level <= 1 {
				icon = "🟠"
			}
			line := icon + " " + clipRunes(p.Text, statusLineRunes)
			if p.Since != nil {
				line += " (с " + clock(*p.Since, now, loc) + ")"
			}
			if used+utf8.RuneCountInString(line) > statusListRunes {
				fmt.Fprintf(&b, "…и ещё %d — смотри панель\n", len(st.Problems)-i)
				break
			}
			used += utf8.RuneCountInString(line) + 1
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\nНоды:")
	for _, s := range []string{"online", "degraded", "offline", "never", "disabled"} {
		if n := st.Nodes[s]; n > 0 || s == "online" || s == "offline" {
			fmt.Fprintf(&b, " %s %d", s, n)
		}
	}
	b.WriteString("\n")
	if len(st.Services) > 0 {
		b.WriteString("Сервисы:")
		for _, sv := range st.Services {
			name := roleNames[sv.Role]
			if name == "" {
				name = sv.Role
			}
			mark := "✅"
			if !sv.Alive {
				mark = "⚠️"
			}
			fmt.Fprintf(&b, " %s %s %s;", mark, name, sv.Version)
		}
		b.WriteString("\n")
	}
	if st.Core.Version != "" {
		b.WriteString("Ядро " + st.Core.Version + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func coreDownCause(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "соединение отклонено"
	case strings.Contains(msg, "x509") || strings.Contains(msg, "tls:") || strings.Contains(msg, "certificate"):
		return "ошибка TLS (проверь серты)"
	case strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout"):
		return "таймаут"
	case strings.Contains(msg, "no such host"):
		return "имя ядра не резолвится"
	case strings.Contains(msg, "connection reset") || strings.Contains(msg, "EOF"):
		return "соединение оборвалось"
	}
	if m := apiStatus.FindStringSubmatch(msg); m != nil {
		return "ядро ответило HTTP " + m[1]
	}
	return "нет ответа"
}
