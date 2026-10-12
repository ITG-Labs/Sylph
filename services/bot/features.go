package bot

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
)

const (
	textTrialGone = "Пробный период недоступен — его можно взять только один раз и только до первой покупки."
	rangeToday    = "today"
	range7        = "7"
	range30       = "30"
	rangePeriod   = "period"
)

func startCode(text string) (string, bool) {
	payload, ok := strings.CutPrefix(text, "/start ")
	if !ok {
		return "", false
	}
	payload = strings.TrimSpace(payload)
	if len(payload) != 22 {
		return "", false
	}
	for _, r := range payload {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_') {
			return "", false
		}
	}
	return payload, true
}

func (a *App) menu(ctx context.Context, tgID int64) (string, *models.InlineKeyboardMarkup) {
	st, err := a.status(ctx, tgID)
	if err != nil {
		a.log.LogAttrs(ctx, slog.LevelWarn, "menu_status_failed", slog.Int64("telegram_id", tgID), slog.String("error", err.Error()))
	}
	trial := err == nil && st.Trial != nil
	return a.menuText(trial), a.menuKeyboard(trial)
}

func (a *App) onInvite(ctx context.Context, tgID, userID int64, code string) (string, *models.InlineKeyboardMarkup) {
	red, err := a.core.RedeemInvite(ctx, userID, code)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrConflict) {
			a.log.LogAttrs(ctx, slog.LevelError, "invite_redeem_failed", slog.Int64("telegram_id", tgID), slog.String("error", err.Error()))
		}
		return a.header(icLock, "Приглашение не сработало") +
			"Ссылка истекла, отозвана или уже использована. Если это ошибка — напиши тому, кто её дал.", keyboard(a.buyRow(), a.backRow())
	}
	a.log.LogAttrs(ctx, slog.LevelInfo, "invite_redeemed", slog.Int64("telegram_id", tgID), slog.Int64("plan_id", red.Plan.ID), slog.Bool("already", red.Already))
	p := red.Plan
	text := a.header(icUnlock, "Тебе открыт тариф") + a.planBlock(p) + "\n\n" +
		a.ic(icInfo) + " Он виден только тебе — в «Купить / продлить»."
	var rows [][]models.InlineKeyboardButton
	if p.Trial {
		rows = append(rows, row(styled(a.btn(icGift, "Попробовать бесплатно", cbTrialAsk), styleSuccess)))
	} else if p.PriceRub > 0 {
		rows = append(rows, row(styled(a.btn(icPlan, planButton(p), cbPlanPrefix+fmt.Sprint(p.ID)), styleSuccess)))
	}
	return text, keyboard(append(rows, a.backRow())...)
}

func (a *App) onTrialAsk(ctx context.Context, tg Telegram, to reply, tgID int64) string {
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "trial", tgID, err)
	}
	if st.Trial == nil {
		return textTrialGone
	}
	a.show(ctx, tg, to, a.trialText(*st.Trial), keyboard(
		row(styled(a.btn(icGift, "Активировать", cbTrialOK), styleSuccess)),
		a.backRow(),
	))
	return ""
}

func (a *App) trialText(p Plan) string {
	var b strings.Builder
	b.WriteString(a.header(icGift, "Пробный период"))
	fmt.Fprintf(&b, "Бесплатно на %s.\n\n", days(p.PeriodDays))
	if p.TrafficLimitBytes != nil {
		fmt.Fprintf(&b, "%s %s трафика\n", a.ic(icTraffic), formatBytes(*p.TrafficLimitBytes))
	} else {
		fmt.Fprintf(&b, "%s без лимита трафика\n", a.ic(icTraffic))
	}
	if p.DeviceLimit != nil {
		fmt.Fprintf(&b, "%s %s\n", a.ic(icDevice), devicesUpTo(*p.DeviceLimit))
	}
	if p.HWIDRequired {
		fmt.Fprintf(&b, "\n%s Работает в Happ и v2RayTun. На одном устройстве пробный период можно взять только один раз.\n", a.ic(icInfo))
	}
	fmt.Fprintf(&b, "\n%s Купишь тариф — он включится сразу, остаток пробного сгорит.", a.ic(icDiamond))
	return b.String()
}

func (a *App) onTrial(ctx context.Context, tg Telegram, to reply, tgID int64) string {
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "trial", tgID, err)
	}
	until, err := a.core.GrantTrial(ctx, st.User.ID)
	if errors.Is(err, ErrConflict) {
		return textTrialGone
	}
	if err != nil {
		return a.fail(ctx, "trial", tgID, err)
	}
	a.log.LogAttrs(ctx, slog.LevelInfo, "trial_activated", slog.Int64("telegram_id", tgID))
	a.show(ctx, tg, to, a.trialDoneText(until, st.User.SubToken), a.statusKeyboard(st.User.SubToken))
	return ""
}

func (a *App) trialDoneText(until time.Time, token string) string {
	return a.header(icParty, "Пробный период включён") +
		"Добавь ссылку в клиент — и всё заработает " + a.ic(icSparkles) + "\n\n" +
		a.ic(icCalendar) + " <b>Действует до</b>\n" + a.when(until) + "\n\n" +
		a.linkBlock("Ссылка подписки", token)
}

func (a *App) onStats(ctx context.Context, tg Telegram, to reply, tgID int64, rng string) string {
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "stats", tgID, err)
	}
	stats, err := a.core.Stats(ctx, st.User.ID)
	if err != nil {
		return a.fail(ctx, "stats", tgID, err)
	}
	text, kb := a.statsView(stats, rng)
	a.show(ctx, tg, to, text, kb)
	return ""
}

func statsRange(s *Stats, rng string) string {
	switch rng {
	case rangeToday, range7, range30:
		return rng
	case rangePeriod:
		if s.PeriodID != nil {
			return rng
		}
	}
	if s.PeriodID != nil {
		return rangePeriod
	}
	return range30
}

func (n NodeStats) bytes(rng string) int64 {
	switch rng {
	case rangeToday:
		return n.Today
	case range7:
		return n.Days7
	case range30:
		return n.Days30
	}
	return n.Period
}

func (a *App) statsView(s *Stats, raw string) (string, *models.InlineKeyboardMarkup) {
	rng := statsRange(s, raw)
	titles := map[string]string{rangeToday: "Сегодня", range7: "За 7 дней", range30: "За 30 дней", rangePeriod: "За текущий период"}
	var b strings.Builder
	b.WriteString(a.header(icChart, "Статистика"))
	fmt.Fprintf(&b, "<b>%s</b>\n\n", titles[rng])
	nodes := slices.Clone(s.Nodes)
	slices.SortStableFunc(nodes, func(x, y NodeStats) int { return cmp.Compare(y.bytes(rng), x.bytes(rng)) })
	var total int64
	for _, n := range nodes {
		v := n.bytes(rng)
		if v <= 0 {
			continue
		}
		total += v
		fmt.Fprintf(&b, "%s %s\n%s\n\n", a.ic(icGlobe), html.EscapeString(n.Name), formatBytes(v))
	}
	if total == 0 {
		fmt.Fprintf(&b, "%s Пока нет трафика за этот срок.", a.ic(icEmpty))
	} else {
		fmt.Fprintf(&b, "%s <b>Всего</b> · %s", a.ic(icTraffic), formatBytes(total))
	}
	tab := func(key, label string) models.InlineKeyboardButton {
		btn := models.InlineKeyboardButton{Text: label, CallbackData: cbStatsPref + key}
		if key == rng {
			btn.Style = stylePrimary
		}
		return btn
	}
	rows := [][]models.InlineKeyboardButton{row(tab(rangeToday, "Сегодня"), tab(range7, "7 дней"), tab(range30, "30 дней"))}
	if s.PeriodID != nil {
		rows = append(rows, row(tab(rangePeriod, "Текущий период")))
	}
	return b.String(), keyboard(append(rows, a.backRow())...)
}
