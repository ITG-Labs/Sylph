package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	NoticeEvery        = time.Minute
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
}

type Notices interface {
	DueNotices(ctx context.Context) ([]Notice, error)
	MarkNotice(ctx context.Context, n Notice, outcome string) error
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
	a.SendNotices(ctx, core, tg)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.SendNotices(ctx, core, tg)
		}
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

func (a *App) SendNotices(ctx context.Context, core Notices, tg Telegram) {
	list, err := core.DueNotices(ctx)
	if errors.Is(err, ErrNotFound) {
		a.log.LogAttrs(ctx, slog.LevelWarn, "notices_unsupported")
		return
	}
	if err != nil {
		a.log.LogAttrs(ctx, slog.LevelWarn, "notices_fetch_failed", slog.String("error", err.Error()))
		return
	}
	for i, n := range list {
		if ctx.Err() != nil {
			return
		}
		if i > 0 && a.noticePause > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(a.noticePause):
			}
		}
		attrs := []slog.Attr{slog.Int64("user_id", n.UserID), slog.String("kind", n.Kind)}
		text, kb := a.noticeView(n)
		if text == "" {
			a.log.LogAttrs(ctx, slog.LevelWarn, "notice_unknown_kind", attrs...)
			continue
		}
		_, err := tg.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: n.TelegramID, Text: text, ParseMode: models.ParseModeHTML, ReplyMarkup: kb, LinkPreviewOptions: noPreview(),
		})
		outcome := outcomeSent
		switch {
		case err == nil:
		case unreachable(err):
			outcome = outcomeUnreachable
		case throttled(err):
			a.log.LogAttrs(ctx, slog.LevelWarn, "notices_throttled", slog.String("error", err.Error()))
			return
		case botBroken(err):
			a.log.LogAttrs(ctx, slog.LevelError, "notices_bot_broken", slog.String("error", err.Error()))
			return
		case errors.Is(err, tgbot.ErrorBadRequest):
			outcome = outcomeFailed
			a.log.LogAttrs(ctx, slog.LevelError, "notice_send_failed", append(attrs, slog.String("error", err.Error()))...)
		default:
			a.log.LogAttrs(ctx, slog.LevelError, "notice_send_failed", append(attrs, slog.String("error", err.Error()))...)
			continue
		}
		markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), noticeMarkTimeout)
		err = core.MarkNotice(markCtx, n, outcome)
		cancel()
		if err != nil {
			a.log.LogAttrs(ctx, slog.LevelError, "notice_mark_failed", append(attrs, slog.String("error", err.Error()))...)
			continue
		}
		a.log.LogAttrs(ctx, slog.LevelInfo, "notice_sent", append(attrs, slog.String("outcome", outcome))...)
	}
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
	switch n.Kind {
	case "traffic_80":
		b.WriteString(a.header(icTraffic, "Израсходовано 80% трафика"))
	case "traffic_95":
		b.WriteString(a.header(icWarn, "Трафик почти закончился"))
	default:
		b.WriteString(a.header(icWarn, "Трафик закончился"))
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
