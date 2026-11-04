package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	NoticeEvery        = time.Minute
	maxNoticeRounds    = 100
	noticeMarkTimeout  = 10 * time.Second
	outcomeSent        = "sent"
	outcomeUnreachable = "unreachable"
	outcomeFailed      = "failed"
)

type Notice struct {
	UserID            int64      `json:"user_id"`
	TelegramID        int64      `json:"telegram_id"`
	Kind              string     `json:"kind"`
	Ref               string     `json:"ref"`
	Trial             bool       `json:"trial"`
	PaidUntil         *time.Time `json:"paid_until"`
	PeriodEnds        *time.Time `json:"period_ends"`
	TrafficUsedBytes  *int64     `json:"traffic_used_bytes"`
	TrafficLimitBytes *int64     `json:"traffic_limit_bytes"`
	PoolID            *int64     `json:"pool_id,omitempty"`
	PoolName          string     `json:"pool_name,omitempty"`
	MultiPool         bool       `json:"multi_pool"`
	TrafficExhausted  bool       `json:"traffic_exhausted"`
}

type Notices interface {
	DueNotices(ctx context.Context) ([]Notice, error)
	MarkNotice(ctx context.Context, n Notice, outcome string) error
	UnnotifiedPayments(ctx context.Context) ([]PaidNotice, error)
	MarkPaymentNotified(ctx context.Context, paymentID string) error
}

func (c *CoreClient) DueNotices(ctx context.Context) ([]Notice, error) {
	var out []Notice
	err := c.do(ctx, http.MethodGet, "/v1/notifications/due", nil, &out)
	return out, err
}

func (c *CoreClient) MarkNotice(ctx context.Context, n Notice, outcome string) error {
	body := map[string]any{"user_id": n.UserID, "kind": n.Kind, "ref": n.Ref, "outcome": outcome}
	return c.do(ctx, http.MethodPost, "/v1/notifications/sent", body, nil)
}

func (a *App) RunNotices(ctx context.Context, core Notices, tg Telegram, every time.Duration) {
	a.SendPaidNotices(ctx, core, tg)
	a.SendNotices(ctx, core, tg)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.SendPaidNotices(ctx, core, tg)
			a.SendNotices(ctx, core, tg)
		}
	}
}

func (a *App) markPaid(ctx context.Context, core Notices, paymentID string) error {
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), noticeMarkTimeout)
	defer cancel()
	return core.MarkPaymentNotified(markCtx, paymentID)
}

func (a *App) SendPaidNotices(ctx context.Context, core Notices, tg Telegram) {
	a.paidMu.Lock()
	defer a.paidMu.Unlock()
	for id := range a.paidUnmarked {
		if err := a.markPaid(ctx, core, id); err != nil && !errors.Is(err, ErrNotFound) {
			a.log.LogAttrs(ctx, slog.LevelError, "payment_notice_mark_failed", slog.String("payment_id", id), slog.String("error", err.Error()))
			return
		}
		delete(a.paidUnmarked, id)
	}
	list, err := core.UnnotifiedPayments(ctx)
	if errors.Is(err, ErrNotFound) {
		a.log.LogAttrs(ctx, slog.LevelWarn, "payment_notices_unsupported")
		return
	}
	if err != nil {
		a.log.LogAttrs(ctx, slog.LevelWarn, "payment_notices_fetch_failed", slog.String("error", err.Error()))
		return
	}
	for i, n := range list {
		if ctx.Err() != nil {
			return
		}
		if a.paidUnmarked[n.PaymentID] {
			continue
		}
		if i > 0 && a.noticePause > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(a.noticePause):
			}
		}
		attrs := []slog.Attr{slog.String("payment_id", n.PaymentID), slog.Int64("telegram_id", n.TelegramID)}
		err := a.NotifyPaid(ctx, tg, n.TelegramID, n.PaidUntil)
		switch {
		case err == nil, unreachable(err), errors.Is(err, tgbot.ErrorBadRequest):
			if err != nil {
				a.log.LogAttrs(ctx, slog.LevelWarn, "payment_notice_undeliverable", append(attrs, slog.String("error", err.Error()))...)
			}
		case throttled(err):
			a.log.LogAttrs(ctx, slog.LevelWarn, "notices_throttled", slog.String("error", err.Error()))
			return
		case botBroken(err):
			a.log.LogAttrs(ctx, slog.LevelError, "notices_bot_broken", slog.String("error", err.Error()))
			return
		default:
			a.log.LogAttrs(ctx, slog.LevelError, "payment_notice_failed", append(attrs, slog.String("error", err.Error()))...)
			continue
		}
		if err := a.markPaid(ctx, core, n.PaymentID); err != nil {
			a.log.LogAttrs(ctx, slog.LevelError, "payment_notice_mark_failed", append(attrs, slog.String("error", err.Error()))...)
			if !errors.Is(err, ErrNotFound) {
				a.paidUnmarked[n.PaymentID] = true
				return
			}
			continue
		}
		a.log.LogAttrs(ctx, slog.LevelInfo, "payment_notice_sent", attrs...)
	}
}

func unreachable(err error) bool {
	return errors.Is(err, tgbot.ErrorForbidden) ||
		errors.Is(err, tgbot.ErrorBadRequest) && strings.Contains(strings.ToLower(err.Error()), "chat not found")
}

func throttled(err error) bool {
	return tgbot.IsTooManyRequestsError(err) || errors.Is(err, tgbot.ErrorTooManyRequests)
}

func botBroken(err error) bool {
	return errors.Is(err, tgbot.ErrorUnauthorized) || errors.Is(err, tgbot.ErrorNotFound)
}

type pendingMark struct {
	notice  Notice
	outcome string
}

type noticeKey struct {
	user int64
	kind string
	ref  string
}

func (a *App) flushUnmarked(ctx context.Context, core Notices) bool {
	a.unmarkedMu.Lock()
	defer a.unmarkedMu.Unlock()
	for key, p := range a.unmarked {
		if err := a.markNotice(ctx, core, p.notice, p.outcome); err != nil {
			if errors.Is(err, ErrNotFound) {
				a.log.LogAttrs(ctx, slog.LevelWarn, "notice_mark_dropped", slog.Int64("user_id", key.user), slog.String("kind", key.kind), slog.String("error", err.Error()))
				delete(a.unmarked, key)
				continue
			}
			a.log.LogAttrs(ctx, slog.LevelError, "notice_mark_failed", slog.Int64("user_id", key.user), slog.String("kind", key.kind), slog.String("error", err.Error()))
			return false
		}
		delete(a.unmarked, key)
	}
	return true
}

func (a *App) markNotice(ctx context.Context, core Notices, n Notice, outcome string) error {
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), noticeMarkTimeout)
	defer cancel()
	return core.MarkNotice(markCtx, n, outcome)
}

func (a *App) rememberUnmarked(n Notice, outcome string) {
	a.unmarkedMu.Lock()
	defer a.unmarkedMu.Unlock()
	a.unmarked[noticeKey{n.UserID, n.Kind, n.Ref}] = pendingMark{notice: n, outcome: outcome}
}

func (a *App) SendNotices(ctx context.Context, core Notices, tg Telegram) {
	if !a.flushUnmarked(ctx, core) {
		return
	}
	attempts := 0
	tried := map[noticeKey]bool{}
	for round := range maxNoticeRounds {
		marked, more := a.sendNoticeRound(ctx, core, tg, &attempts, tried)
		if !more || marked == 0 || ctx.Err() != nil {
			if round > 0 {
				a.log.LogAttrs(ctx, slog.LevelInfo, "notices_drained", slog.Int("rounds", round+1), slog.Int("attempts", attempts))
			}
			return
		}
	}
	a.log.LogAttrs(ctx, slog.LevelWarn, "notices_round_limit", slog.Int("rounds", maxNoticeRounds), slog.Int("attempts", attempts))
}

func (a *App) sendNoticeRound(ctx context.Context, core Notices, tg Telegram, attempts *int, tried map[noticeKey]bool) (int, bool) {
	list, err := core.DueNotices(ctx)
	if errors.Is(err, ErrNotFound) {
		a.log.LogAttrs(ctx, slog.LevelWarn, "notices_unsupported")
		return 0, false
	}
	if err != nil {
		a.log.LogAttrs(ctx, slog.LevelWarn, "notices_fetch_failed", slog.String("error", err.Error()))
		return 0, false
	}
	if len(list) == 0 {
		return 0, false
	}
	marked, fresh := 0, 0
	for _, n := range list {
		if ctx.Err() != nil {
			return marked, false
		}
		key := noticeKey{n.UserID, n.Kind, n.Ref}
		if tried[key] {
			continue
		}
		tried[key] = true
		fresh++
		*attempts++
		if *attempts > 1 && a.noticePause > 0 {
			select {
			case <-ctx.Done():
				return marked, false
			case <-time.After(a.noticePause):
			}
		}
		attrs := []slog.Attr{slog.Int64("user_id", n.UserID), slog.String("kind", n.Kind)}
		text, kb := a.noticeView(n)
		if text == "" {
			a.log.LogAttrs(ctx, slog.LevelWarn, "notice_unknown_kind", attrs...)
			continue
		}
		_, err := sendMessage(ctx, tg, &tgbot.SendMessageParams{
			ChatID: n.TelegramID, Text: text, ParseMode: models.ParseModeHTML, ReplyMarkup: kb, LinkPreviewOptions: noPreview(),
		})
		outcome := outcomeSent
		switch {
		case err == nil:
		case unreachable(err):
			outcome = outcomeUnreachable
		case throttled(err):
			a.log.LogAttrs(ctx, slog.LevelWarn, "notices_throttled", slog.String("error", err.Error()))
			return marked, false
		case botBroken(err):
			a.log.LogAttrs(ctx, slog.LevelError, "notices_bot_broken", slog.String("error", err.Error()))
			return marked, false
		case errors.Is(err, tgbot.ErrorBadRequest):
			outcome = outcomeFailed
			a.log.LogAttrs(ctx, slog.LevelError, "notice_send_failed", append(attrs, slog.String("error", err.Error()))...)
		default:
			a.log.LogAttrs(ctx, slog.LevelError, "notice_send_failed", append(attrs, slog.String("error", err.Error()))...)
			continue
		}
		if err := a.markNotice(ctx, core, n, outcome); err != nil {
			a.log.LogAttrs(ctx, slog.LevelError, "notice_mark_failed", append(attrs, slog.String("error", err.Error()))...)
			if !errors.Is(err, ErrNotFound) {
				a.rememberUnmarked(n, outcome)
				return marked, false
			}
			continue
		}
		marked++
		a.log.LogAttrs(ctx, slog.LevelInfo, "notice_sent", append(attrs, slog.String("outcome", outcome))...)
	}
	return marked, fresh > 0
}

func (a *App) noticeView(n Notice) (string, *models.InlineKeyboardMarkup) {
	var text string
	switch n.Kind {
	case "traffic_80", "traffic_95", "traffic_out":
		text = a.trafficNotice(n)
	case "expiry_3d", "expiry_1d", "expiry_1h":
		text = a.expiryNotice(n)
	case "expired":
		text = a.expiredNotice(n)
	default:
		return "", nil
	}
	return text, keyboard(a.buyRow(), a.statusRow())
}

func (a *App) trafficNotice(n Notice) string {
	var b strings.Builder
	of, subject := "трафика", "Трафик"
	switch {
	case n.MultiPool && n.PoolName != "":
		of, subject = "трафика «"+html.EscapeString(n.PoolName)+"»", "Трафик «"+html.EscapeString(n.PoolName)+"»"
	case n.MultiPool:
		of, subject = "основного трафика", "Основной трафик"
	}
	switch n.Kind {
	case "traffic_80":
		b.WriteString(a.header(icTraffic, "Израсходовано 80% "+of))
	case "traffic_95":
		b.WriteString(a.header(icWarn, subject+" почти закончился"))
	default:
		b.WriteString(a.header(icWarn, subject+" закончился"))
	}
	if n.TrafficUsedBytes != nil && n.TrafficLimitBytes != nil {
		used, limit := *n.TrafficUsedBytes, *n.TrafficLimitBytes
		fmt.Fprintf(&b, "%s\n%s из %s\n\n", trafficBar(used, limit), formatBytes(used), formatBytes(limit))
	}
	if n.PeriodEnds == nil {
		return strings.TrimRight(b.String(), "\n")
	}
	if n.Kind != "traffic_out" {
		fmt.Fprintf(&b, "%s <b>Текущий период до</b>\n%s", a.ic(icClock), a.when(*n.PeriodEnds))
		return b.String()
	}
	if n.MultiPool && !n.TrafficExhausted {
		fmt.Fprintf(&b, "Остальные серверы работают. Этот трафик обновится с новым периодом — %s.", a.when(*n.PeriodEnds))
		return b.String()
	}
	if n.PaidUntil != nil && n.PaidUntil.After(*n.PeriodEnds) {
		fmt.Fprintf(&b, "Следующий оплаченный период начнётся %s.\nНе хочешь ждать — оплати сейчас, новый период начнётся сразу.", a.when(*n.PeriodEnds))
	} else {
		fmt.Fprintf(&b, "VPN не работает: трафик на этот период исчерпан, период — до %s.\nПродлишь — новый период начнётся сразу, с полным трафиком.", a.when(*n.PeriodEnds))
	}
	return b.String()
}

func (a *App) expiryNotice(n Notice) string {
	var b strings.Builder
	switch {
	case n.Trial && n.Kind == "expiry_1h":
		b.WriteString(a.header(icGift, "Пробный период закончится через час"))
	case n.Trial:
		b.WriteString(a.header(icGift, "Пробный период скоро закончится") + "Осталось меньше суток.\n\n")
	case n.Kind == "expiry_1h":
		b.WriteString(a.header(icClock, "Подписка закончится через час"))
	case n.Kind == "expiry_1d":
		b.WriteString(a.header(icClock, "Подписка скоро закончится") + "Осталось меньше суток.\n\n")
	default:
		b.WriteString(a.header(icClock, "Подписка заканчивается") + "Осталось меньше 3 дней.\n\n")
	}
	if n.PaidUntil != nil {
		title := "Оплачено до"
		if n.Trial {
			title = "Пробный период до"
		}
		fmt.Fprintf(&b, "%s <b>%s</b>\n%s\n\n", a.ic(icCalendar), title, a.when(*n.PaidUntil))
	}
	if n.Trial {
		b.WriteString("Понравилось — выбери тариф.")
	} else {
		b.WriteString("Продли, чтобы не остаться без VPN.")
	}
	return b.String()
}

func (a *App) expiredNotice(n Notice) string {
	if n.Trial {
		return a.header(icGift, "Пробный период закончился") + "Выбери тариф, чтобы продолжить."
	}
	return a.header(icCross, "Подписка закончилась") + "VPN не работает. Продли, чтобы снова подключиться."
}
