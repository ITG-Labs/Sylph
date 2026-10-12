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
	cbDevices    = "dev:list"
	cbDevicePref = "dev:i:"
	cbDeviceDel  = "dev:d:"
	cbDevicesAsk = "dev:ask"
	cbDevicesOK  = "dev:ok"
	cbLinkAsk    = "link:ask"
	cbLinkOK     = "link:ok"
	cbPlanPrefix = "buy:"
	cbChkPrefix  = "chk:"
)

const (
	stylePrimary = "primary"
	styleSuccess = "success"
	styleDanger  = "danger"
)

const rule = "━━━━━━━━━━━━━━━━━━━━"

const (
	textPlanGone     = "Этот тариф больше не продаётся — выбери другой."
	textPayDisabled  = "Оплата сейчас недоступна — попробуй позже."
	textProviderDown = "ЮKassa не отвечает — попробуй через минуту."
	textFailed       = "Что-то пошло не так, попробуй ещё раз чуть позже."
	textNotFound     = "Платёж не найден."
	textDeviceGone   = "Устройство не найдено — возможно, его уже отвязали."
	textPending      = "Оплата ещё не пришла. Если уже оплатил — подожди минуту и нажми ещё раз: ЮKassa подтверждает не сразу."
)

type icon struct {
	plain string
	id    string
}

var (
	icShield   = icon{"🛡", "6030445631921721471"}
	icDiamond  = icon{"💎", "6037083366438737901"}
	icPlan     = icon{"📦", "5884479287171485878"}
	icTraffic  = icon{"📊", "5936143551854285132"}
	icDevice   = icon{"📱", "5942734685976138521"}
	icTrash    = icon{"🗑", "6039522349517115015"}
	icLink     = icon{"🔗", "6028171274939797252"}
	icCycle    = icon{"🔁", "6030657343744644592"}
	icClock    = icon{"⏰", "5983150113483134607"}
	icCalendar = icon{"📅", "5890937706803894250"}
	icTag      = icon{"🏷", "5890727932011223292"}
	icPay      = icon{"💳", "5904359114531675993"}
	icCheck    = icon{"✅", "5774022692642492953"}
	icCross    = icon{"❌", "5774077015388852135"}
	icWarn     = icon{"❗️", "6030563507299160824"}
	icInfo     = icon{"ℹ️", "6028435952299413210"}
	icEmpty    = icon{"⚪️", "5884332803016891855"}
	icParty    = icon{"🎉", "6041731551845159060"}
	icWave     = icon{"👋", "5985478698722136468"}
	icSparkles = icon{"✨", "5890925363067886150"}
	icReturn   = icon{"↩️", "6039539366177541657"}
	icBack     = icon{"⬅️", "5960671702059848143"}
	icGear     = icon{"⚙️", "6032742198179532882"}
	icClient   = icon{"🧩", "5837069325034331827"}
	icRecent   = icon{"🕓", "5775896410780079073"}
)

func (a *App) UseCustomEmoji(on bool) {
	a.customEmoji = on
}

func (a *App) ic(i icon) string {
	if a.customEmoji {
		return `<tg-emoji emoji-id="` + i.id + `">` + i.plain + `</tg-emoji>`
	}
	return i.plain
}

func (a *App) btn(i icon, text, data string) models.InlineKeyboardButton {
	if a.customEmoji {
		return models.InlineKeyboardButton{Text: text, CallbackData: data, IconCustomEmojiID: i.id}
	}
	return models.InlineKeyboardButton{Text: i.plain + " " + text, CallbackData: data}
}

func (a *App) urlBtn(i icon, text, url string) models.InlineKeyboardButton {
	b := a.btn(i, text, "")
	b.URL = url
	return b
}

func styled(b models.InlineKeyboardButton, style string) models.InlineKeyboardButton {
	b.Style = style
	return b
}

func keyboard(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func row(b ...models.InlineKeyboardButton) []models.InlineKeyboardButton {
	return b
}

func (a *App) header(i icon, title string) string {
	return a.ic(i) + " <b>" + title + "</b>\n" + rule + "\n\n"
}

func (a *App) menuKeyboard() *models.InlineKeyboardMarkup {
	return keyboard(
		row(styled(a.btn(icShield, "Моя подписка", cbStatus), stylePrimary)),
		row(styled(a.btn(icDiamond, "Купить / продлить", cbBuy), styleSuccess)),
		row(a.btn(icDevice, "Устройства", cbDevices), a.btn(icCycle, "Сменить ссылку", cbLinkAsk)),
	)
}

func (a *App) buyRow() []models.InlineKeyboardButton {
	return row(styled(a.btn(icDiamond, "Купить / продлить", cbBuy), styleSuccess))
}

func (a *App) backRow() []models.InlineKeyboardButton {
	return row(a.btn(icBack, "Меню", cbMenu))
}

func (a *App) statusRow() []models.InlineKeyboardButton {
	return row(styled(a.btn(icShield, "Моя подписка", cbStatus), stylePrimary))
}

func (a *App) confirmKeyboard(i icon, yes, data string) *models.InlineKeyboardMarkup {
	return keyboard(row(styled(a.btn(i, yes, data), styleDanger)), row(a.btn(icBack, "Отмена", cbMenu)))
}

func (a *App) menuText() string {
	return a.header(icWave, "Привет!") +
		"Здесь всё для твоей подписки:\n\n" +
		a.ic(icShield) + "  статус, трафик и «оплачено до»\n" +
		a.ic(icDiamond) + "  покупка и продление тарифа\n" +
		a.ic(icLink) + "  ссылка для клиента\n" +
		a.ic(icDevice) + "  привязанные устройства"
}

func (a *App) statusKeyboard(token string) *models.InlineKeyboardMarkup {
	return keyboard(row(a.urlBtn(icLink, "Открыть ссылку", a.link(token))), a.buyRow(), a.backRow())
}

func deviceLabel(d Device, n int) string {
	var parts []string
	switch {
	case d.Model != "":
		parts = append(parts, d.Model)
	case d.OS != "":
		parts = append(parts, d.OS)
	}
	if client, _, _ := strings.Cut(d.UserAgent, "/"); client != "" {
		parts = append(parts, client)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("Устройство %d", n)
	}
	label := strings.Join(parts, " · ")
	if r := []rune(label); len(r) > 40 {
		label = string(r[:40]) + "…"
	}
	return label
}

func (a *App) devicesView(st *Status, devices []Device, notice string) (string, *models.InlineKeyboardMarkup) {
	var b strings.Builder
	b.WriteString(a.header(icDevice, "Устройства"))
	if notice != "" {
		b.WriteString(notice + "\n\n")
	}
	var rows [][]models.InlineKeyboardButton
	if len(devices) == 0 {
		b.WriteString("Пока ни одного устройства.\n\n")
	} else {
		switch {
		case st.Plan != nil && st.Plan.DeviceLimit != nil:
			fmt.Fprintf(&b, "Привязано <b>%d из %d</b>\n\n", len(devices), *st.Plan.DeviceLimit)
		case st.Plan != nil:
			fmt.Fprintf(&b, "Привязано <b>%d</b> · без лимита\n\n", len(devices))
		default:
			fmt.Fprintf(&b, "Привязано <b>%d</b>\n\n", len(devices))
		}
		b.WriteString("Нажми на устройство, чтобы посмотреть его или отвязать.\n\n")
		for i, d := range devices {
			rows = append(rows, row(a.btn(icDevice, deviceLabel(d, i+1), cbDevicePref+strconv.FormatInt(d.ID, 10))))
		}
		rows = append(rows, row(styled(a.btn(icTrash, "Отвязать все", cbDevicesAsk), styleDanger)))
	}
	b.WriteString(a.ic(icInfo) + " Устройство привязывается, когда клиент с HWID (Happ, v2RayTun и другие) обновляет подписку.")
	rows = append(rows, a.backRow())
	return b.String(), keyboard(rows...)
}

func orUnknown(s string) string {
	if s == "" {
		return "клиент не сообщил"
	}
	return html.EscapeString(s)
}

func (a *App) deviceView(d Device, n int) (string, *models.InlineKeyboardMarkup) {
	var b strings.Builder
	b.WriteString(a.header(icDevice, html.EscapeString(deviceLabel(Device{Model: d.Model, OS: d.OS}, n))))
	fmt.Fprintf(&b, "%s <b>Система</b>\n%s\n\n", a.ic(icGear), orUnknown(d.OS))
	fmt.Fprintf(&b, "%s <b>Клиент</b>\n%s\n\n", a.ic(icClient), orUnknown(d.UserAgent))
	fmt.Fprintf(&b, "%s <b>Привязано</b>\n%s\n\n", a.ic(icLink), a.when(d.FirstSeen))
	fmt.Fprintf(&b, "%s <b>Последний раз</b>\n%s", a.ic(icRecent), a.when(d.LastSeen))
	return b.String(), keyboard(
		row(styled(a.btn(icTrash, "Отвязать", cbDeviceDel+strconv.FormatInt(d.ID, 10)), styleDanger)),
		row(a.btn(icBack, "К устройствам", cbDevices)),
	)
}

func (a *App) canceledText() string {
	return a.header(icCross, "Платёж не прошёл") +
		"Платёж отменён или истёк.\n\nВыбери тариф заново — будет новая ссылка на оплату."
}

func (a *App) mismatchText() string {
	return a.header(icWarn, "Проблема с платежом") +
		"С этим платежом что-то не так — подписку по нему не продлили.\n\nНапиши в поддержку, разберёмся."
}

func (a *App) devicesAskText() string {
	return a.header(icDevice, "Сбросить устройства?") +
		"Нужно, если сменил телефон и упёрся в лимит.\n\n" +
		a.ic(icInfo) + " Устройства привяжутся заново, когда клиент обновит подписку."
}

func (a *App) devicesDoneText() string {
	return a.header(icCheck, "Устройства сброшены") +
		"Обнови подписку в клиенте на нужном устройстве — оно привяжется заново."
}

func (a *App) linkAskText() string {
	return a.header(icCycle, "Сменить ссылку?") +
		"Старая ссылка и все клиенты, настроенные по ней, перестанут работать.\n\n" +
		"Новую придётся заново добавить в клиент.\n\n" +
		a.ic(icInfo) + " Трафик и оплаченные дни сохранятся."
}

func (a *App) planGoneText() string {
	return a.header(icDiamond, "Тарифы") + textPlanGone
}

func (a *App) buyRulesText() string {
	return "<blockquote expandable>" + a.ic(icInfo) + " <b>Как работает продление</b>\n\n" +
		"Тот же тариф, что сейчас, встанет в очередь — начнётся после всех уже оплаченных периодов.\n\n" +
		"Другой тариф включится сразу после оплаты, а остаток текущего сгорит.\n\n" +
		"Если трафик кончился, доступ закрыт до конца периода. Оплатишь сейчас — любой тариф включится сразу, " +
		"а уже оплаченные периоды сдвинутся следом и не пропадут.</blockquote>"
}

func (a *App) when(t time.Time) string {
	return fmt.Sprintf(`<tg-time unix="%d" format="Dt">%s</tg-time>`, t.Unix(), t.In(a.loc).Format("02.01.2006 15:04"))
}

func (a *App) link(token string) string {
	return a.subsURL + "/" + token
}

func (a *App) linkBlock(title, token string) string {
	return a.ic(icLink) + " <b>" + title + "</b>\nТапни, чтобы скопировать:\n\n<code>" + html.EscapeString(a.link(token)) + "</code>"
}

func exhausted(st *Status) bool {
	return st.Subscription != nil && st.Plan != nil && st.Plan.TrafficLimitBytes != nil && st.Subscription.TrafficUsedBytes >= *st.Plan.TrafficLimitBytes
}

func (a *App) statusText(st *Status) string {
	var b strings.Builder
	b.WriteString(a.header(icShield, "Моя подписка"))
	sub, plan := st.Subscription, st.Plan
	if sub == nil || plan == nil {
		fmt.Fprintf(&b, "%s <b>Подписки нет</b>\n\nВыбери тариф в «Купить / продлить» — подписка включится сразу после оплаты.\n\n", a.ic(icEmpty))
		b.WriteString(a.linkBlock("Ссылка подписки", st.User.SubToken))
		return b.String()
	}
	hasNext := sub.PaidUntil != nil && sub.PaidUntil.After(sub.ExpiresAt)
	switch {
	case exhausted(st) && hasNext:
		fmt.Fprintf(&b, "%s <b>Трафик исчерпан</b>\nСледующий оплаченный период начнётся %s.\nНе хочешь ждать — оплати сейчас, новый период начнётся сразу.\n", a.ic(icWarn), a.when(sub.ExpiresAt))
	case exhausted(st):
		fmt.Fprintf(&b, "%s <b>Трафик исчерпан</b>\nДоступ закрыт до %s.\nОплатишь сейчас — новый период начнётся сразу.\n", a.ic(icWarn), a.when(sub.ExpiresAt))
	default:
		fmt.Fprintf(&b, "%s <b>Активна</b>\n", a.ic(icCheck))
	}
	fmt.Fprintf(&b, "%s Тариф «%s»\n\n", a.ic(icPlan), html.EscapeString(plan.Name))
	fmt.Fprintf(&b, "%s <b>Трафик</b>\n", a.ic(icTraffic))
	if plan.TrafficLimitBytes != nil {
		fmt.Fprintf(&b, "%s\n%s из %s\n\n", trafficBar(sub.TrafficUsedBytes, *plan.TrafficLimitBytes), formatBytes(sub.TrafficUsedBytes), formatBytes(*plan.TrafficLimitBytes))
	} else {
		fmt.Fprintf(&b, "%s · без лимита\n\n", formatBytes(sub.TrafficUsedBytes))
	}
	fmt.Fprintf(&b, "%s <b>Устройства</b>\n", a.ic(icDevice))
	if plan.DeviceLimit != nil {
		fmt.Fprintf(&b, "%d из %d\n\n", st.Devices, *plan.DeviceLimit)
	} else {
		fmt.Fprintf(&b, "%d · без лимита\n\n", st.Devices)
	}
	fmt.Fprintf(&b, "%s <b>Текущий период до</b>\n%s\n\n", a.ic(icClock), a.when(sub.ExpiresAt))
	if sub.PaidUntil != nil {
		fmt.Fprintf(&b, "%s <b>Оплачено до</b>\n%s\n\n", a.ic(icCalendar), a.when(*sub.PaidUntil))
	}
	b.WriteString(a.linkBlock("Ссылка подписки", st.User.SubToken))
	return b.String()
}

func trafficBar(used, limit int64) string {
	const width = 10
	filled := 0
	if limit > 0 {
		filled = int(min(used, limit) * width / limit)
		if used > 0 && filled == 0 {
			filled = 1
		}
	}
	return strings.Repeat("▰", filled) + strings.Repeat("▱", width-filled)
}

func (a *App) planBlock(p Plan) string {
	traffic := "без лимита трафика"
	if p.TrafficLimitBytes != nil {
		traffic = formatBytes(*p.TrafficLimitBytes) + " трафика"
	}
	devices := "устройств без лимита"
	if p.DeviceLimit != nil {
		devices = devicesUpTo(*p.DeviceLimit)
	}
	return fmt.Sprintf("%s <b>%s</b>\n%s %d ₽ за %s\n%s %s\n%s %s",
		a.ic(icPlan), html.EscapeString(p.Name),
		a.ic(icTag), p.PriceRub, days(p.PeriodDays),
		a.ic(icTraffic), traffic,
		a.ic(icDevice), devices)
}

func planButton(p Plan) string {
	return fmt.Sprintf("%s · %d ₽", p.Name, p.PriceRub)
}

func (a *App) plansView(st *Status, plans []Plan) (string, *models.InlineKeyboardMarkup) {
	var rows [][]models.InlineKeyboardButton
	var blocks []string
	if renew := offSaleCurrent(st, plans); renew != nil {
		blocks = append(blocks, a.planBlock(*renew)+"\n"+a.ic(icCycle)+" <i>твой текущий, снят с продажи</i>")
		rows = append(rows, row(styled(a.btn(icCycle, "Продлить текущий: "+planButton(*renew), cbPlanPrefix+strconv.FormatInt(renew.ID, 10)), stylePrimary)))
	}
	for _, p := range plans {
		blocks = append(blocks, a.planBlock(p))
		rows = append(rows, row(styled(a.btn(icPlan, planButton(p), cbPlanPrefix+strconv.FormatInt(p.ID, 10)), styleSuccess)))
	}
	if len(blocks) == 0 {
		return a.header(icDiamond, "Тарифы") + "Сейчас тарифов в продаже нет.", keyboard(a.backRow())
	}
	rows = append(rows, a.backRow())
	return a.header(icDiamond, "Тарифы") + strings.Join(blocks, "\n\n") + "\n\n" + a.buyRulesText(), keyboard(rows...)
}

func offSaleCurrent(st *Status, plans []Plan) *Plan {
	if st.Subscription == nil || st.Plan == nil || st.Plan.PriceRub <= 0 {
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
	b.WriteString(a.header(icTag, "Оплата"))
	fmt.Fprintf(&b, "%s <b>%s</b>\n%d ₽ за %s\n\n", a.ic(icPlan), html.EscapeString(plan.Name), pay.AmountRub, days(plan.PeriodDays))
	if exhausted(st) {
		fmt.Fprintf(&b, "%s Трафик исчерпан — новый период начнётся сразу после оплаты, трафик снова появится.\n\n", a.ic(icInfo))
	} else if st.Subscription != nil && st.Plan != nil {
		if st.Plan.ID == plan.ID {
			fmt.Fprintf(&b, "%s Новый период встанет в очередь — после всех уже оплаченных.\n\n", a.ic(icInfo))
		} else {
			fmt.Fprintf(&b, "%s Сейчас у тебя тариф «%s» — его остаток сгорит, новый включится сразу после оплаты.\n\n", a.ic(icWarn), html.EscapeString(st.Plan.Name))
		}
	}
	fmt.Fprintf(&b, "%s Нажми «Оплатить» — картой или через СБП\n\n", a.ic(icPay))
	fmt.Fprintf(&b, "%s Вернись сюда — подписка продлится сама за пару минут\n\n", a.ic(icReturn))
	fmt.Fprintf(&b, "%s Не хочешь ждать — жми «Я оплатил»", a.ic(icCheck))
	return b.String(), keyboard(
		row(styled(a.urlBtn(icPay, fmt.Sprintf("Оплатить %d ₽", pay.AmountRub), pay.ConfirmationURL), styleSuccess)),
		row(styled(a.btn(icCheck, "Я оплатил", cbChkPrefix+pay.PaymentID), stylePrimary)),
		a.backRow(),
	)
}

func (a *App) paidText(paidUntil *time.Time) string {
	text := a.header(icParty, "Оплата прошла!") + "Спасибо! Подписка уже работает " + a.ic(icSparkles)
	if paidUntil != nil {
		text += "\n\n" + a.ic(icCalendar) + " <b>Оплачено до</b>\n" + a.when(*paidUntil)
	}
	return text
}

func (a *App) rotatedText(token string) string {
	return a.header(icCheck, "Ссылка обновлена") +
		"Старая больше не работает — добавь новую в клиент заново.\n\n" + a.linkBlock("Новая ссылка", token)
}

func plural(n int, one, few, many string) string {
	n10, n100 := n%10, n%100
	switch {
	case n10 == 1 && n100 != 11:
		return one
	case n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14):
		return few
	default:
		return many
	}
}

func days(n int) string {
	return strconv.Itoa(n) + " " + plural(n, "день", "дня", "дней")
}

func devicesUpTo(n int) string {
	return "до " + strconv.Itoa(n) + " " + plural(n, "устройства", "устройств", "устройств")
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
