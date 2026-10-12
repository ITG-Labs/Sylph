package bot

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
)

const (
	cbStatus     = "menu:sub"
	cbBuy        = "menu:buy"
	cbMenu       = "menu:main"
	cbDevicesAsk = "dev:ask"
	cbDevicesOK  = "dev:ok"
	cbLinkAsk    = "link:ask"
	cbLinkOK     = "link:ok"
	cbPlanPrefix = "buy:"
	cbChkPrefix  = "chk:"
)

const (
	textMenu = "👋 Привет! Здесь можно купить или продлить подписку, получить ссылку для клиента и управлять устройствами."

	textBuyRules = "Тот же тариф, что сейчас, встанет в очередь после текущего периода. Другой тариф включится сразу, а остаток текущего сгорит."

	textNoPlans       = "Сейчас тарифов в продаже нет."
	textPlanGone      = "Этот тариф больше не продаётся — выбери другой."
	textPayDisabled   = "Оплата сейчас недоступна — попробуй позже."
	textProviderDown  = "ЮKassa не отвечает — попробуй через минуту."
	textFailed        = "Что-то пошло не так, попробуй ещё раз чуть позже."
	textNotFound      = "Платёж не найден."
	textPending       = "Оплата ещё не пришла. Если уже оплатил — подожди минуту и нажми ещё раз: ЮKassa подтверждает не сразу."
	textCanceled      = "❌ Платёж отменён или истёк. Выбери тариф заново — будет новая ссылка на оплату."
	textMismatch      = "⚠️ С этим платежом что-то не так — подписку по нему не продлили. Напиши в поддержку, разберёмся."
	textDevicesAsk    = "📱 Сбросить привязанные устройства?\n\nЭто нужно, если сменил телефон и упёрся в лимит. Устройства привяжутся заново, когда клиент обновит подписку."
	textDevicesDone   = "✅ Устройства сброшены. Обнови подписку в клиенте на нужном устройстве."
	textLinkAsk       = "🔄 Сменить ссылку подписки?\n\nСтарая ссылка и все клиенты, настроенные по ней, перестанут работать — новую нужно будет заново добавить в клиент. Трафик и оплаченные дни сохранятся."
	textLinkDonePart1 = "✅ Ссылка сменена. Новая ссылка — нажми, чтобы скопировать, и добавь её в клиент заново:"
)

func button(text, data string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

func keyboard(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func row(b ...models.InlineKeyboardButton) []models.InlineKeyboardButton {
	return b
}

func menuKeyboard() *models.InlineKeyboardMarkup {
	return keyboard(
		row(button("📊 Моя подписка", cbStatus)),
		row(button("💳 Купить / продлить", cbBuy)),
		row(button("📱 Сбросить устройства", cbDevicesAsk), button("🔄 Сменить ссылку", cbLinkAsk)),
	)
}

func backRow() []models.InlineKeyboardButton {
	return row(button("⬅️ Меню", cbMenu))
}

func (a *App) when(t time.Time) string {
	return t.In(a.loc).Format("02.01.2006 15:04")
}

func (a *App) link(token string) string {
	return a.subsURL + "/" + token
}

func exhausted(st *Status) bool {
	return st.Subscription != nil && st.Plan != nil && st.Plan.TrafficLimitBytes != nil && st.Subscription.TrafficUsedBytes >= *st.Plan.TrafficLimitBytes
}

func (a *App) statusText(st *Status) string {
	var b strings.Builder
	b.WriteString("📊 <b>Моя подписка</b>\n\n")
	sub, plan := st.Subscription, st.Plan
	if sub == nil || plan == nil {
		b.WriteString("Статус: ❌ подписки нет — выбери тариф в «Купить / продлить».\n")
	} else {
		fmt.Fprintf(&b, "Тариф: <b>%s</b>\n", html.EscapeString(plan.Name))
		hasNext := sub.PaidUntil != nil && sub.PaidUntil.After(sub.ExpiresAt)
		switch {
		case exhausted(st) && hasNext:
			fmt.Fprintf(&b, "Статус: ⛔ трафик исчерпан — следующий период начнётся %s\n", a.when(sub.ExpiresAt))
		case exhausted(st):
			fmt.Fprintf(&b, "Статус: ⛔ трафик исчерпан до %s — новый оплаченный период начнётся сразу\n", a.when(sub.ExpiresAt))
		default:
			fmt.Fprintf(&b, "Статус: ✅ активна, текущий период до %s\n", a.when(sub.ExpiresAt))
		}
		if plan.TrafficLimitBytes != nil {
			fmt.Fprintf(&b, "Трафик: %s из %s\n", formatBytes(sub.TrafficUsedBytes), formatBytes(*plan.TrafficLimitBytes))
		} else {
			fmt.Fprintf(&b, "Трафик: %s (без лимита)\n", formatBytes(sub.TrafficUsedBytes))
		}
		if plan.DeviceLimit != nil {
			fmt.Fprintf(&b, "Устройства: %d из %d\n", st.Devices, *plan.DeviceLimit)
		} else {
			fmt.Fprintf(&b, "Устройства: %d (без лимита)\n", st.Devices)
		}
		if sub.PaidUntil != nil {
			fmt.Fprintf(&b, "Оплачено до: %s\n", a.when(*sub.PaidUntil))
		}
	}
	fmt.Fprintf(&b, "\n🔗 Ссылка подписки — нажми, чтобы скопировать:\n<code>%s</code>", html.EscapeString(a.link(st.User.SubToken)))
	return b.String()
}

func planLine(p Plan) string {
	traffic := "без лимита трафика"
	if p.TrafficLimitBytes != nil {
		traffic = formatBytes(*p.TrafficLimitBytes)
	}
	devices := "устройств без лимита"
	if p.DeviceLimit != nil {
		devices = fmt.Sprintf("до %d устройств", *p.DeviceLimit)
	}
	return fmt.Sprintf("• <b>%s</b> — %d ₽ за %d дн. · %s · %s", html.EscapeString(p.Name), p.PriceRub, p.PeriodDays, traffic, devices)
}

func planButton(p Plan) string {
	return fmt.Sprintf("%s — %d ₽", p.Name, p.PriceRub)
}

func (a *App) plansView(st *Status, plans []Plan) (string, *models.InlineKeyboardMarkup) {
	var rows [][]models.InlineKeyboardButton
	var lines []string
	if renew := offSaleCurrent(st, plans); renew != nil {
		lines = append(lines, planLine(*renew)+" — твой текущий, снят с продажи")
		rows = append(rows, row(button("🔁 Продлить текущий: "+planButton(*renew), cbPlanPrefix+strconv.FormatInt(renew.ID, 10))))
	}
	for _, p := range plans {
		lines = append(lines, planLine(p))
		rows = append(rows, row(button(planButton(p), cbPlanPrefix+strconv.FormatInt(p.ID, 10))))
	}
	rows = append(rows, backRow())
	if len(lines) == 0 {
		return textNoPlans, keyboard(backRow())
	}
	return "💳 <b>Тарифы</b>\n\n" + strings.Join(lines, "\n") + "\n\n" + textBuyRules, keyboard(rows...)
}

func offSaleCurrent(st *Status, plans []Plan) *Plan {
	if st.Subscription == nil || st.Plan == nil {
		return nil
	}
	for _, p := range plans {
		if p.ID == st.Plan.ID {
			return nil
		}
	}
	return st.Plan
}

func (a *App) paymentView(st *Status, plan Plan, pay *Payment) (string, *models.InlineKeyboardMarkup) {
	var b strings.Builder
	fmt.Fprintf(&b, "🧾 <b>Оплата</b>\n\nТариф: <b>%s</b> — %d ₽ за %d дн.\n", html.EscapeString(plan.Name), pay.AmountRub, plan.PeriodDays)
	if st.Subscription != nil && st.Plan != nil && !exhausted(st) {
		if st.Plan.ID == plan.ID {
			b.WriteString("ℹ️ Новый период встанет в очередь после текущего.\n")
		} else {
			fmt.Fprintf(&b, "⚠️ Сейчас у тебя тариф «%s» — его остаток сгорит, новый включится сразу после оплаты.\n", html.EscapeString(st.Plan.Name))
		}
	}
	b.WriteString("\nОплати по кнопке ниже. Подписка продлится сама в течение пары минут, а «Я оплатил» проверит оплату сразу.")
	return b.String(), keyboard(
		row(models.InlineKeyboardButton{Text: fmt.Sprintf("💳 Оплатить %d ₽", pay.AmountRub), URL: pay.ConfirmationURL}),
		row(button("✅ Я оплатил", cbChkPrefix+pay.PaymentID)),
		backRow(),
	)
}

func (a *App) paidText(paidUntil *time.Time) string {
	if paidUntil == nil {
		return "✅ <b>Оплата прошла!</b>"
	}
	return "✅ <b>Оплата прошла!</b>\n\nОплачено до " + a.when(*paidUntil) + "."
}

func formatBytes(n int64) string {
	const (
		mib = 1 << 20
		gib = 1 << 30
		tib = 1 << 40
	)
	switch {
	case n >= tib:
		return decimal(float64(n)/tib) + " ТБ"
	case n >= gib:
		return decimal(float64(n)/gib) + " ГБ"
	default:
		return strconv.FormatInt(n/mib, 10) + " МБ"
	}
}

func decimal(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return strings.Replace(s, ".", ",", 1)
}
