package service

import (
	"context"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxTelegramNameRunes = 64

var telegramUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

type TelegramProfileInput struct {
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func invisibleMark(r rune) bool {
	switch {
	case r == '\u2028', r == '\u2029', r == '\u200b', r == '\u200e', r == '\u200f', r == '\u2060', r == '\ufeff', r == '\u061c':
		return true
	case r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
		return true
	}
	return false
}

func cleanTelegramName(v string) string {
	var b strings.Builder
	for _, r := range v {
		if unicode.IsControl(r) || invisibleMark(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > maxTelegramNameRunes {
		out = strings.TrimSpace(string([]rune(out)[:maxTelegramNameRunes]))
	}
	return out
}

func cleanTelegramProfile(in TelegramProfileInput) store.TelegramProfile {
	p := store.TelegramProfile{FirstName: cleanTelegramName(in.FirstName), LastName: cleanTelegramName(in.LastName)}
	if nick := strings.TrimPrefix(strings.TrimSpace(in.Username), "@"); telegramUsernamePattern.MatchString(nick) {
		p.Username = &nick
	}
	return p
}

func (s *Service) SaveTelegramProfile(ctx context.Context, userID int64, in TelegramProfileInput) error {
	return s.store.SaveTelegramProfile(ctx, userID, cleanTelegramProfile(in))
}
