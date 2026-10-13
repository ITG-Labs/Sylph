package bot

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Telegram interface {
	SendMessage(ctx context.Context, params *tgbot.SendMessageParams) (*models.Message, error)
	EditMessageText(ctx context.Context, params *tgbot.EditMessageTextParams) (*models.Message, error)
	AnswerCallbackQuery(ctx context.Context, params *tgbot.AnswerCallbackQueryParams) (bool, error)
}

type App struct {
	core          Core
	subsURL       string
	loc           *time.Location
	log           *slog.Logger
	customEmoji   bool
	checkCooldown time.Duration
	checkedMu     sync.Mutex
	lastCheck     map[string]time.Time
}

func NewApp(core Core, subsURL string, loc *time.Location, log *slog.Logger) *App {
	return &App{core: core, subsURL: strings.TrimRight(subsURL, "/"), loc: loc, log: log, checkCooldown: 3 * time.Second, lastCheck: map[string]time.Time{}}
}

func (a *App) checkTooSoon(tgID int64, paymentID string) bool {
	if a.checkCooldown <= 0 {
		return false
	}
	key := strconv.FormatInt(tgID, 10) + ":" + paymentID
	now := time.Now()
	a.checkedMu.Lock()
	defer a.checkedMu.Unlock()
	if last, ok := a.lastCheck[key]; ok && now.Sub(last) < a.checkCooldown {
		return true
	}
	if len(a.lastCheck) > 1000 {
		for k, at := range a.lastCheck {
			if now.Sub(at) >= a.checkCooldown {
				delete(a.lastCheck, k)
			}
		}
	}
	a.lastCheck[key] = now
	return false
}

type reply struct {
	chatID int64
	msgID  int
}

func (a *App) Handle(ctx context.Context, tg Telegram, u *models.Update) {
	switch {
	case u.Message != nil:
		a.onMessage(ctx, tg, u.Message)
	case u.CallbackQuery != nil:
		a.onCallback(ctx, tg, u.CallbackQuery)
	}
}

func (a *App) onMessage(ctx context.Context, tg Telegram, m *models.Message) {
	if m.Chat.Type != models.ChatTypePrivate || m.From == nil {
		return
	}
	u, err := a.core.Register(ctx, m.From.ID)
	if err != nil {
		a.log.LogAttrs(ctx, slog.LevelError, "user_register_failed", slog.Int64("telegram_id", m.From.ID), slog.String("error", err.Error()))
		a.send(ctx, tg, m.Chat.ID, textFailed, nil)
		return
	}
	if code, ok := startCode(m.Text); ok {
		text, kb := a.onInvite(ctx, m.From.ID, u.ID, code)
		a.send(ctx, tg, m.Chat.ID, text, kb)
		return
	}
	text, kb := a.menu(ctx, m.From.ID)
	a.send(ctx, tg, m.Chat.ID, text, kb)
}

func (a *App) onCallback(ctx context.Context, tg Telegram, q *models.CallbackQuery) {
	to := reply{chatID: q.From.ID}
	switch {
	case q.Message.Message != nil:
		if q.Message.Message.Chat.Type != models.ChatTypePrivate {
			return
		}
		to = reply{chatID: q.Message.Message.Chat.ID, msgID: q.Message.Message.ID}
	case q.Message.InaccessibleMessage != nil && q.Message.InaccessibleMessage.Chat.Type != models.ChatTypePrivate:
		return
	}
	tgID := q.From.ID
	data := q.Data
	var alert string
	switch {
	case data == cbMenu:
		text, kb := a.menu(ctx, tgID)
		a.show(ctx, tg, to, text, kb)
	case data == cbTrialAsk:
		alert = a.onTrialAsk(ctx, tg, to, tgID)
	case data == cbTrialOK:
		alert = a.onTrial(ctx, tg, to, tgID)
	case strings.HasPrefix(data, cbStatsPref):
		alert = a.onStats(ctx, tg, to, tgID, strings.TrimPrefix(data, cbStatsPref))
	case data == cbStatus:
		alert = a.onStatus(ctx, tg, to, tgID)
	case data == cbBuy:
		alert = a.onPlans(ctx, tg, to, tgID)
	case strings.HasPrefix(data, cbPlanPrefix):
		alert = a.onBuy(ctx, tg, to, tgID, strings.TrimPrefix(data, cbPlanPrefix))
	case strings.HasPrefix(data, cbChkPrefix):
		alert = a.onCheck(ctx, tg, to, tgID, strings.TrimPrefix(data, cbChkPrefix))
	case data == cbDevices:
		alert = a.onDevices(ctx, tg, to, tgID, "")
	case strings.HasPrefix(data, cbDevicePref):
		alert = a.onDevice(ctx, tg, to, tgID, strings.TrimPrefix(data, cbDevicePref))
	case strings.HasPrefix(data, cbDeviceDel):
		alert = a.onDeviceRemove(ctx, tg, to, tgID, strings.TrimPrefix(data, cbDeviceDel))
	case data == cbDevicesAsk:
		a.show(ctx, tg, to, a.devicesAskText(), a.confirmKeyboard(icTrash, "Да, сбросить", cbDevicesOK))
	case data == cbDevicesOK:
		alert = a.onReset(ctx, tg, to, tgID)
	case data == cbLinkAsk:
		a.show(ctx, tg, to, a.linkAskText(), a.confirmKeyboard(icCycle, "Да, сменить", cbLinkOK))
	case data == cbLinkOK:
		alert = a.onRotate(ctx, tg, to, tgID)
	}
	params := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	if alert != "" {
		params.Text, params.ShowAlert = alert, true
	}
	if _, err := tg.AnswerCallbackQuery(ctx, params); err != nil {
		a.log.LogAttrs(ctx, slog.LevelWarn, "telegram_answer_failed", slog.String("error", err.Error()))
	}
}

func (a *App) status(ctx context.Context, tgID int64) (*Status, error) {
	st, err := a.core.Status(ctx, tgID)
	if !errors.Is(err, ErrNotFound) {
		return st, err
	}
	if _, err := a.core.Register(ctx, tgID); err != nil {
		return nil, err
	}
	return a.core.Status(ctx, tgID)
}

func (a *App) fail(ctx context.Context, what string, tgID int64, err error) string {
	switch {
	case errors.Is(err, ErrConflict):
		return textPlanGone
	case errors.Is(err, ErrPaymentsDisabled):
		return textPayDisabled
	case errors.Is(err, ErrProviderUnavailable):
		return textProviderDown
	}
	a.log.LogAttrs(ctx, slog.LevelError, what+"_failed", slog.Int64("telegram_id", tgID), slog.String("error", err.Error()))
	return textFailed
}

func (a *App) onStatus(ctx context.Context, tg Telegram, to reply, tgID int64) string {
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "status", tgID, err)
	}
	a.show(ctx, tg, to, a.statusText(st), a.statusKeyboard(st.User.SubToken))
	return ""
}

func (a *App) onPlans(ctx context.Context, tg Telegram, to reply, tgID int64) string {
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "plans", tgID, err)
	}
	plans, err := a.core.Plans(ctx, st.User.ID)
	if err != nil {
		return a.fail(ctx, "plans", tgID, err)
	}
	text, kb := a.plansView(st, plans)
	a.show(ctx, tg, to, text, kb)
	return ""
}

func (a *App) onBuy(ctx context.Context, tg Telegram, to reply, tgID int64, raw string) string {
	planID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || planID <= 0 {
		return textPlanGone
	}
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "buy", tgID, err)
	}
	plans, err := a.core.Plans(ctx, st.User.ID)
	if err != nil {
		return a.fail(ctx, "buy", tgID, err)
	}
	var plan *Plan
	for i := range plans {
		if plans[i].ID == planID {
			plan = &plans[i]
		}
	}
	if renew := offSaleCurrent(st, plans); plan == nil && renew != nil && renew.ID == planID {
		plan = renew
	}
	if plan == nil {
		a.show(ctx, tg, to, a.planGoneText(), keyboard(a.buyRow(), a.backRow()))
		return ""
	}
	pay, err := a.core.CreatePayment(ctx, st.User.ID, plan.ID)
	if err != nil {
		return a.fail(ctx, "payment_create", tgID, err)
	}
	a.log.LogAttrs(ctx, slog.LevelInfo, "payment_link_sent",
		slog.Int64("telegram_id", tgID),
		slog.String("payment_id", pay.PaymentID),
		slog.Int64("plan_id", plan.ID))
	text, kb := a.paymentView(st, *plan, pay)
	a.show(ctx, tg, to, text, kb)
	return ""
}

func (a *App) onCheck(ctx context.Context, tg Telegram, to reply, tgID int64, paymentID string) string {
	if !validPaymentID(paymentID) {
		return textNotFound
	}
	if a.checkTooSoon(tgID, paymentID) {
		return textPending
	}
	chk, err := a.core.CheckPayment(ctx, paymentID)
	if errors.Is(err, ErrNotFound) {
		return textNotFound
	}
	if err != nil {
		return a.fail(ctx, "payment_check", tgID, err)
	}
	if chk.TelegramID == nil || *chk.TelegramID != tgID {
		a.log.LogAttrs(ctx, slog.LevelWarn, "payment_check_foreign", slog.Int64("telegram_id", tgID), slog.String("payment_id", paymentID))
		return textNotFound
	}
	a.log.LogAttrs(ctx, slog.LevelInfo, "payment_checked",
		slog.Int64("telegram_id", tgID),
		slog.String("payment_id", paymentID),
		slog.String("status", chk.Status),
		slog.Bool("applied", chk.Applied))
	switch chk.Status {
	case "succeeded":
		a.show(ctx, tg, to, a.paidText(chk.PaidUntil), keyboard(a.statusRow(), a.backRow()))
	case "canceled":
		a.show(ctx, tg, to, a.canceledText(), keyboard(a.buyRow(), a.backRow()))
	case "mismatch":
		a.show(ctx, tg, to, a.mismatchText(), keyboard(a.backRow()))
	default:
		return textPending
	}
	return ""
}

func (a *App) onDevices(ctx context.Context, tg Telegram, to reply, tgID int64, notice string) string {
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "devices", tgID, err)
	}
	devices, err := a.core.Devices(ctx, st.User.ID)
	if err != nil {
		return a.fail(ctx, "devices", tgID, err)
	}
	text, kb := a.devicesView(st, devices, notice)
	a.show(ctx, tg, to, text, kb)
	return ""
}

func deviceID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0
}

func (a *App) onDevice(ctx context.Context, tg Telegram, to reply, tgID int64, raw string) string {
	id, ok := deviceID(raw)
	if !ok {
		return textDeviceGone
	}
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "devices", tgID, err)
	}
	devices, err := a.core.Devices(ctx, st.User.ID)
	if err != nil {
		return a.fail(ctx, "devices", tgID, err)
	}
	for i, d := range devices {
		if d.ID == id {
			text, kb := a.deviceView(d, i+1)
			a.show(ctx, tg, to, text, kb)
			return ""
		}
	}
	return textDeviceGone
}

func (a *App) onDeviceRemove(ctx context.Context, tg Telegram, to reply, tgID int64, raw string) string {
	id, ok := deviceID(raw)
	if !ok {
		return textDeviceGone
	}
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "device_remove", tgID, err)
	}
	if err := a.core.RemoveDevice(ctx, st.User.ID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return textDeviceGone
		}
		return a.fail(ctx, "device_remove", tgID, err)
	}
	a.log.LogAttrs(ctx, slog.LevelInfo, "device_removed", slog.Int64("telegram_id", tgID), slog.Int64("device_id", id))
	return a.onDevices(ctx, tg, to, tgID, a.ic(icCheck)+" Устройство отвязано.")
}

func (a *App) onReset(ctx context.Context, tg Telegram, to reply, tgID int64) string {
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "devices_reset", tgID, err)
	}
	if err := a.core.ResetDevices(ctx, st.User.ID); err != nil {
		return a.fail(ctx, "devices_reset", tgID, err)
	}
	a.log.LogAttrs(ctx, slog.LevelInfo, "devices_reset", slog.Int64("telegram_id", tgID))
	a.show(ctx, tg, to, a.devicesDoneText(), keyboard(a.backRow()))
	return ""
}

func (a *App) onRotate(ctx context.Context, tg Telegram, to reply, tgID int64) string {
	st, err := a.status(ctx, tgID)
	if err != nil {
		return a.fail(ctx, "link_rotate", tgID, err)
	}
	token, err := a.core.Rotate(ctx, st.User.ID)
	if err != nil {
		return a.fail(ctx, "link_rotate", tgID, err)
	}
	a.log.LogAttrs(ctx, slog.LevelInfo, "link_rotated", slog.Int64("telegram_id", tgID))
	a.show(ctx, tg, to, a.rotatedText(token), keyboard(a.backRow()))
	return ""
}

func (a *App) NotifyPaid(ctx context.Context, tg Telegram, telegramID int64, paidUntil *time.Time) error {
	_, err := tg.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: telegramID, Text: a.paidText(paidUntil), ParseMode: models.ParseModeHTML,
		ReplyMarkup: keyboard(a.statusRow()),
	})
	return err
}

func (a *App) show(ctx context.Context, tg Telegram, to reply, text string, kb *models.InlineKeyboardMarkup) {
	if to.msgID != 0 {
		_, err := tg.EditMessageText(ctx, &tgbot.EditMessageTextParams{
			ChatID: to.chatID, MessageID: to.msgID, Text: text, ParseMode: models.ParseModeHTML, ReplyMarkup: kb,
			LinkPreviewOptions: noPreview(),
		})
		if err == nil || strings.Contains(err.Error(), "message is not modified") {
			return
		}
		a.log.LogAttrs(ctx, slog.LevelWarn, "telegram_edit_failed", slog.String("error", err.Error()))
	}
	a.send(ctx, tg, to.chatID, text, kb)
}

func (a *App) send(ctx context.Context, tg Telegram, chatID int64, text string, kb *models.InlineKeyboardMarkup) {
	params := &tgbot.SendMessageParams{ChatID: chatID, Text: text, ParseMode: models.ParseModeHTML, LinkPreviewOptions: noPreview()}
	if kb != nil {
		params.ReplyMarkup = kb
	}
	if _, err := tg.SendMessage(ctx, params); err != nil {
		a.log.LogAttrs(ctx, slog.LevelError, "telegram_send_failed", slog.Int64("chat_id", chatID), slog.String("error", err.Error()))
	}
}

func noPreview() *models.LinkPreviewOptions {
	disabled := true
	return &models.LinkPreviewOptions{IsDisabled: &disabled}
}
