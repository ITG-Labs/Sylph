package bot

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	defaultConfigTTL = time.Minute
	defaultMemberTTL = 10 * time.Minute
	configRetry      = 10 * time.Second
	maxMembers       = 10000
)

func (a *App) config(ctx context.Context) BotConfig {
	a.cfgMu.Lock()
	cached, until := a.cfg, a.cfgUntil
	a.cfgMu.Unlock()
	if time.Now().Before(until) {
		if cached != nil {
			return *cached
		}
		return BotConfig{}
	}
	cfg, err := a.core.BotConfig(ctx)
	now := time.Now()
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	if err == nil {
		a.cfg, a.cfgUntil = cfg, now.Add(a.configTTL)
		return *cfg
	}
	a.cfgUntil = now.Add(min(configRetry, a.configTTL))
	if now.Sub(a.cfgErrAt) >= time.Minute {
		a.cfgErrAt = now
		a.log.LogAttrs(ctx, slog.LevelWarn, "bot_config_failed", slog.Bool("stale", a.cfg != nil), slog.String("error", err.Error()))
	}
	if a.cfg != nil {
		return *a.cfg
	}
	return BotConfig{}
}

func (a *App) forgetConfig() {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	a.cfg, a.cfgUntil = nil, time.Time{}
}

func (a *App) subscribed(ctx context.Context, tg Telegram, ch ChannelConfig, tgID int64, fresh bool) bool {
	now := time.Now()
	a.memMu.Lock()
	at, ok := a.members[tgID]
	a.memMu.Unlock()
	if !fresh && ok && now.Sub(at) < a.memberTTL {
		return true
	}
	m, err := tg.GetChatMember(ctx, &tgbot.GetChatMemberParams{ChatID: ch.Chat, UserID: tgID})
	if err != nil {
		a.channelFailed(ctx, err)
		return true
	}
	in := isMember(m)
	a.memMu.Lock()
	defer a.memMu.Unlock()
	if !in {
		delete(a.members, tgID)
		return false
	}
	if len(a.members) >= maxMembers {
		for id, seen := range a.members {
			if now.Sub(seen) >= a.memberTTL {
				delete(a.members, id)
			}
		}
		if len(a.members) >= maxMembers {
			clear(a.members)
		}
	}
	a.members[tgID] = now
	return true
}

func isMember(m *models.ChatMember) bool {
	if m == nil {
		return false
	}
	switch m.Type {
	case models.ChatMemberTypeOwner, models.ChatMemberTypeAdministrator, models.ChatMemberTypeMember:
		return true
	case models.ChatMemberTypeRestricted:
		return m.Restricted != nil && m.Restricted.IsMember
	}
	return false
}

func (a *App) channelFailed(ctx context.Context, err error) {
	a.memMu.Lock()
	quiet := time.Since(a.chanErrAt) < time.Minute
	if !quiet {
		a.chanErrAt = time.Now()
	}
	a.memMu.Unlock()
	if !quiet {
		a.log.LogAttrs(ctx, slog.LevelError, "channel_check_failed", slog.String("error", err.Error()))
	}
}

func (a *App) gate(ctx context.Context, tg Telegram, tgID int64, u *User, code string, fresh bool) (string, *models.InlineKeyboardMarkup, bool) {
	if u == nil {
		return "", nil, false
	}
	cfg := a.config(ctx)
	if cfg.Terms.Enabled && u.TermsVersion < cfg.Terms.Version {
		text, kb := a.termsScreen(cfg, u.TermsVersion > 0, code)
		return text, kb, true
	}
	if cfg.Channel.Enabled && !a.subscribed(ctx, tg, cfg.Channel, tgID, fresh) {
		text, kb := a.channelScreen(cfg, code)
		return text, kb, true
	}
	return "", nil, false
}

func (a *App) afterGates(ctx context.Context, tg Telegram, to reply, tgID int64, u *User, code string) {
	if u == nil {
		a.show(ctx, tg, to, textFailed, nil)
		return
	}
	if text, kb, gated := a.gate(ctx, tg, tgID, u, code, false); gated {
		a.show(ctx, tg, to, text, kb)
		return
	}
	if code != "" {
		text, kb := a.onInvite(ctx, tgID, u.ID, code)
		a.show(ctx, tg, to, text, kb)
		return
	}
	text, kb := a.menu(ctx, tgID)
	a.show(ctx, tg, to, text, kb)
}

func withCode(data, code string) string {
	if code == "" {
		return data
	}
	return data + ":" + code
}

func validCode(code string) bool {
	if len(code) != 22 {
		return false
	}
	for _, r := range code {
		if !isPayloadRune(r) {
			return false
		}
	}
	return true
}

func (a *App) onTermsAccept(ctx context.Context, tg Telegram, to reply, tgID int64, u *User, raw string) string {
	ver, code, _ := strings.Cut(raw, ":")
	version, err := strconv.Atoi(ver)
	if err != nil || version < 1 || (code != "" && !validCode(code)) {
		return ""
	}
	if u == nil {
		return textFailed
	}
	if err := a.core.AcceptTerms(ctx, u.ID, version); err != nil {
		if !errors.Is(err, ErrConflict) {
			return a.fail(ctx, "terms", tgID, err)
		}
		a.forgetConfig()
		cfg := a.config(ctx)
		if !cfg.Terms.Enabled {
			a.afterGates(ctx, tg, to, tgID, u, code)
			return ""
		}
		text, kb := a.termsScreen(cfg, true, code)
		a.show(ctx, tg, to, text, kb)
		return "Соглашение обновилось — прочитай новую версию и прими её."
	}
	a.log.LogAttrs(ctx, slog.LevelInfo, "terms_accepted", slog.Int64("telegram_id", tgID), slog.Int("version", version))
	u.TermsVersion = version
	a.afterGates(ctx, tg, to, tgID, u, code)
	return ""
}

func (a *App) onJoined(ctx context.Context, tg Telegram, to reply, tgID int64, u *User, code string) string {
	if code != "" && !validCode(code) {
		return ""
	}
	cfg := a.config(ctx)
	if cfg.Channel.Enabled && !a.subscribed(ctx, tg, cfg.Channel, tgID, true) {
		return "Не вижу подписки — подпишись на канал и нажми ещё раз."
	}
	a.afterGates(ctx, tg, to, tgID, u, code)
	return ""
}

func (a *App) onHelp(ctx context.Context, tg Telegram, to reply, tgID int64, u *User, code string) {
	cfg := a.config(ctx)
	if !cfg.Help.Enabled {
		a.afterGates(ctx, tg, to, tgID, u, code)
		return
	}
	text, kb := a.helpScreen(cfg, code)
	a.show(ctx, tg, to, text, kb)
}

func (a *App) termsScreen(cfg BotConfig, again bool, code string) (string, *models.InlineKeyboardMarkup) {
	title, lead := "Пользовательское соглашение", "Чтобы пользоваться сервисом, прочитай пользовательское соглашение и прими его."
	if again {
		title, lead = "Соглашение обновилось", "Мы обновили пользовательское соглашение — прочитай новую версию и прими её, чтобы продолжить."
	}
	if cfg.Terms.Text != "" {
		lead = cfg.Terms.Text
	}
	text := a.header(icTerms, title) + lead + "\n\n" + a.ic(icInfo) + " Нажимая «Принимаю», ты соглашаешься с его условиями."
	rows := [][]models.InlineKeyboardButton{
		row(a.urlBtn(icTerms, "Открыть соглашение", cfg.Terms.URL)),
		row(styled(a.btn(icCheck, "Принимаю", withCode(cbTermsPref+strconv.Itoa(cfg.Terms.Version), code)), styleSuccess)),
	}
	if cfg.Help.Enabled {
		rows = append(rows, a.helpRow(code))
	}
	return text, keyboard(rows...)
}

func (a *App) channelScreen(cfg BotConfig, code string) (string, *models.InlineKeyboardMarkup) {
	lead := "Чтобы пользоваться ботом, подпишись на наш канал — там новости и важные объявления."
	if cfg.Channel.Text != "" {
		lead = cfg.Channel.Text
	}
	text := a.header(icChannel, "Подпишись на канал") + lead + "\n\n" + a.ic(icInfo) + " Подписался — нажми «Я подписался»."
	rows := [][]models.InlineKeyboardButton{
		row(a.urlBtn(icChannel, "Открыть канал", cfg.Channel.URL)),
		row(styled(a.btn(icCheck, "Я подписался", cbJoinedPref+code), styleSuccess)),
	}
	if cfg.Help.Enabled {
		rows = append(rows, a.helpRow(code))
	}
	return text, keyboard(rows...)
}

func (a *App) helpScreen(cfg BotConfig, code string) (string, *models.InlineKeyboardMarkup) {
	var rows [][]models.InlineKeyboardButton
	for _, b := range cfg.Help.Buttons {
		rows = append(rows, row(models.InlineKeyboardButton{Text: b.Label, URL: b.URL}))
	}
	return a.header(icHelp, "Помощь") + cfg.Help.Text, keyboard(append(rows, row(a.btn(icBack, "Назад", cbGatePref+code)))...)
}
