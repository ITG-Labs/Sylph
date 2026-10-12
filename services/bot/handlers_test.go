package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type sent struct {
	method   string
	chatID   any
	msgID    int
	text     string
	html     bool
	keyboard [][]models.InlineKeyboardButton
	alert    bool
}

type fakeTelegram struct {
	mu      sync.Mutex
	calls   []sent
	editErr error
}

func (f *fakeTelegram) record(s sent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func keyboardOf(m models.ReplyMarkup) [][]models.InlineKeyboardButton {
	if k, ok := m.(*models.InlineKeyboardMarkup); ok && k != nil {
		return k.InlineKeyboard
	}
	return nil
}

func (f *fakeTelegram) SendMessage(_ context.Context, p *tgbot.SendMessageParams) (*models.Message, error) {
	f.record(sent{method: "sendMessage", chatID: p.ChatID, text: p.Text, html: p.ParseMode == models.ParseModeHTML, keyboard: keyboardOf(p.ReplyMarkup)})
	return &models.Message{}, nil
}

func (f *fakeTelegram) EditMessageText(_ context.Context, p *tgbot.EditMessageTextParams) (*models.Message, error) {
	f.record(sent{method: "editMessageText", chatID: p.ChatID, msgID: p.MessageID, text: p.Text, html: p.ParseMode == models.ParseModeHTML, keyboard: keyboardOf(p.ReplyMarkup)})
	return &models.Message{}, f.editErr
}

func (f *fakeTelegram) AnswerCallbackQuery(_ context.Context, p *tgbot.AnswerCallbackQueryParams) (bool, error) {
	f.record(sent{method: "answerCallbackQuery", text: p.Text, alert: p.ShowAlert})
	return true, nil
}

func (f *fakeTelegram) only(t *testing.T, method string) []sent {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sent
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeTelegram) last(t *testing.T) sent {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].method != "answerCallbackQuery" {
			return f.calls[i]
		}
	}
	t.Fatal("no message sent or edited")
	return sent{}
}

type fakeCore struct {
	mu        sync.Mutex
	status    *Status
	statusErr error
	plans     []Plan
	payment   *Payment
	payErr    error
	check     *Check
	checkErr  error
	rotated   string
	calls     []string
	created   [][2]int64
	resetFor  []int64
	rotateFor []int64
}

func (c *fakeCore) note(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, s)
}

func (c *fakeCore) Register(_ context.Context, tg int64) (*User, error) {
	c.note("register")
	if c.status == nil {
		c.status = &Status{User: User{ID: 7, SubToken: "tok"}}
	}
	return &c.status.User, nil
}

func (c *fakeCore) Status(_ context.Context, tg int64) (*Status, error) {
	c.note("status")
	if c.statusErr != nil {
		return nil, c.statusErr
	}
	if c.status == nil {
		return nil, ErrNotFound
	}
	return c.status, nil
}

func (c *fakeCore) Plans(context.Context) ([]Plan, error) {
	c.note("plans")
	return c.plans, nil
}

func (c *fakeCore) CreatePayment(_ context.Context, userID, planID int64) (*Payment, error) {
	c.note("create")
	c.created = append(c.created, [2]int64{userID, planID})
	return c.payment, c.payErr
}

func (c *fakeCore) CheckPayment(_ context.Context, id string) (*Check, error) {
	c.note("check:" + id)
	return c.check, c.checkErr
}

func (c *fakeCore) Rotate(_ context.Context, userID int64) (string, error) {
	c.note("rotate")
	c.rotateFor = append(c.rotateFor, userID)
	return c.rotated, nil
}

func (c *fakeCore) ResetDevices(_ context.Context, userID int64) error {
	c.note("reset")
	c.resetFor = append(c.resetFor, userID)
	return nil
}

func (c *fakeCore) called(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.calls {
		if s == name {
			return true
		}
	}
	return false
}

const tgUser = int64(1001)

func newTestApp(t *testing.T, core *fakeCore) (*App, *fakeTelegram) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	return NewApp(core, "https://subs.example.com", loc, slog.New(slog.NewJSONHandler(io.Discard, nil))), &fakeTelegram{}
}

func message(text string, chatType models.ChatType) *models.Update {
	return &models.Update{Message: &models.Message{ID: 10, Text: text, From: &models.User{ID: tgUser}, Chat: models.Chat{ID: tgUser, Type: chatType}}}
}

func callback(data string) *models.Update {
	return &models.Update{CallbackQuery: &models.CallbackQuery{
		ID: "cb-1", From: models.User{ID: tgUser}, Data: data,
		Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: &models.Message{ID: 55, Chat: models.Chat{ID: tgUser, Type: models.ChatTypePrivate}}},
	}}
}

func buttons(k [][]models.InlineKeyboardButton) map[string]models.InlineKeyboardButton {
	out := map[string]models.InlineKeyboardButton{}
	for _, row := range k {
		for _, b := range row {
			key := b.CallbackData
			if key == "" {
				key = b.URL
			}
			out[key] = b
		}
	}
	return out
}

func mustContain(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(text, p) {
			t.Fatalf("text lacks %q:\n%s", p, text)
		}
	}
}

func i64(v int64) *int64 { return &v }
func iptr(v int) *int    { return &v }

func at(s string) time.Time {
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return tm
}

func tptr(t time.Time) *time.Time { return &t }

func activeStatus() *Status {
	return &Status{
		User:         User{ID: 7, SubToken: "tok"},
		Subscription: &Period{PlanID: 2, StartsAt: at("2026-10-01T10:00:00Z"), ExpiresAt: at("2026-10-31T10:00:00Z"), TrafficUsedBytes: 1 << 30, PaidUntil: tptr(at("2026-11-30T10:00:00Z"))},
		Plan:         &Plan{ID: 2, Name: "Месяц", PriceRub: 199, PeriodDays: 30, TrafficLimitBytes: i64(100 << 30), DeviceLimit: iptr(3), OnSale: true},
		Devices:      1,
	}
}

func TestStartRegistersAndShowsTheMenu(t *testing.T) {
	core := &fakeCore{}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	if !core.called("register") {
		t.Fatal("/start did not register the user")
	}
	msgs := tg.only(t, "sendMessage")
	if len(msgs) != 1 || msgs[0].chatID != tgUser {
		t.Fatalf("messages = %+v", msgs)
	}
	b := buttons(msgs[0].keyboard)
	for _, data := range []string{"menu:sub", "menu:buy", "dev:ask", "link:ask"} {
		if _, ok := b[data]; !ok {
			t.Fatalf("menu lacks %s: %+v", data, msgs[0].keyboard)
		}
	}
	if b["menu:sub"].Style != "primary" || b["menu:buy"].Style != "success" {
		t.Fatalf("menu buttons are not colored: %+v", msgs[0].keyboard)
	}
	mustContain(t, msgs[0].text, "<b>", "━━")
}

func TestAnyTextShowsTheMenu(t *testing.T) {
	core := &fakeCore{}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, message("привет", models.ChatTypePrivate))
	if len(tg.only(t, "sendMessage")) != 1 || !core.called("register") {
		t.Fatalf("calls = %+v / %v", tg.calls, core.calls)
	}
}

func TestGroupsAreIgnored(t *testing.T) {
	core := &fakeCore{}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, message("/start", models.ChatTypeGroup))
	app.Handle(context.Background(), tg, message("/start", models.ChatTypeSupergroup))
	cb := callback("menu:sub")
	cb.CallbackQuery.Message.Message.Chat.Type = models.ChatTypeGroup
	app.Handle(context.Background(), tg, cb)
	if len(core.calls) != 0 || len(tg.only(t, "sendMessage"))+len(tg.only(t, "editMessageText")) != 0 {
		t.Fatalf("group update handled: core %v, telegram %+v", core.calls, tg.calls)
	}
}

func TestStatusShowsTrafficDevicesPaidUntilAndTheLink(t *testing.T) {
	core := &fakeCore{status: activeStatus()}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	if len(tg.only(t, "answerCallbackQuery")) != 1 {
		t.Fatal("callback not answered")
	}
	m := tg.last(t)
	if m.method != "editMessageText" || m.msgID != 55 || !m.html {
		t.Fatalf("status message = %+v", m)
	}
	mustContain(t, m.text, "✅ <b>Активна</b>", "«Месяц»", "1 ГБ из 100 ГБ", "▰▱▱▱▱▱▱▱▱▱", "Устройства", "1 из 3", "Текущий период до",
		tgTime(at("2026-10-31T10:00:00Z")), "Оплачено до", tgTime(at("2026-11-30T10:00:00Z")), "<code>https://subs.example.com/tok</code>")
	if b := buttons(m.keyboard); b["menu:buy"].Style != "success" {
		t.Fatalf("buy button is not green: %+v", m.keyboard)
	}
}

func TestTrafficBar(t *testing.T) {
	for _, tc := range []struct {
		used, limit int64
		want        string
	}{
		{0, 100, "▱▱▱▱▱▱▱▱▱▱"},
		{1, 100, "▰▱▱▱▱▱▱▱▱▱"},
		{25, 100, "▰▰▱▱▱▱▱▱▱▱"},
		{99, 100, "▰▰▰▰▰▰▰▰▰▱"},
		{100, 100, "▰▰▰▰▰▰▰▰▰▰"},
		{250, 100, "▰▰▰▰▰▰▰▰▰▰"},
	} {
		if got := trafficBar(tc.used, tc.limit); got != tc.want {
			t.Fatalf("trafficBar(%d, %d) = %q, want %q", tc.used, tc.limit, got, tc.want)
		}
	}
}

func TestPlurals(t *testing.T) {
	for n, want := range map[int]string{1: "1 день", 2: "2 дня", 4: "4 дня", 5: "5 дней", 11: "11 дней", 21: "21 день", 30: "30 дней", 365: "365 дней", 112: "112 дней", 22: "22 дня"} {
		if got := days(n); got != want {
			t.Fatalf("days(%d) = %q, want %q", n, got, want)
		}
	}
	for n, want := range map[int]string{1: "до 1 устройства", 3: "до 3 устройств", 5: "до 5 устройств", 11: "до 11 устройств", 21: "до 21 устройства"} {
		if got := devicesUpTo(n); got != want {
			t.Fatalf("devicesUpTo(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDatesFollowTheUsersClock(t *testing.T) {
	app, _ := newTestApp(t, &fakeCore{})
	got := app.when(at("2026-11-30T10:00:00Z"))
	want := `<tg-time unix="1796032800" format="Dt">30.11.2026 13:00</tg-time>`
	if got != want {
		t.Fatalf("when = %q, want %q", got, want)
	}
}

func TestStatusWhenTrafficIsExhausted(t *testing.T) {
	st := activeStatus()
	st.Subscription.TrafficUsedBytes = 100 << 30
	core := &fakeCore{status: st}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	mustContain(t, tg.last(t).text, "❗️ <b>Трафик исчерпан</b>", "Следующий оплаченный период начнётся "+tgTime(st.Subscription.ExpiresAt), "Не хочешь ждать")
	st.Subscription.PaidUntil = tptr(st.Subscription.ExpiresAt)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	m := tg.last(t).text
	mustContain(t, m, "❗️ <b>Трафик исчерпан</b>", "Доступ закрыт до "+tgTime(st.Subscription.ExpiresAt), "Оплатишь сейчас — новый период начнётся сразу")
	if strings.Contains(m, "оплаченный период начнётся сразу") {
		t.Fatalf("status still reads as if a period starts by itself:\n%s", m)
	}
}

func TestStatusWithoutLimitsAndWithoutSubscription(t *testing.T) {
	st := activeStatus()
	st.Plan.TrafficLimitBytes, st.Plan.DeviceLimit = nil, nil
	st.Subscription.TrafficUsedBytes = 1536 << 20
	core := &fakeCore{status: st}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	mustContain(t, tg.last(t).text, "1,5 ГБ · без лимита", "1 · без лимита")
	core.status = &Status{User: User{ID: 7, SubToken: "tok"}}
	app.Handle(context.Background(), tg, callback("menu:sub"))
	m := tg.last(t)
	mustContain(t, m.text, "⚪️ <b>Подписки нет</b>", "https://subs.example.com/tok")
	if strings.Contains(m.text, "Трафик") {
		t.Fatalf("traffic shown without a subscription:\n%s", m.text)
	}
}

func TestStatusOfAnUnknownUserRegistersFirst(t *testing.T) {
	core := &fakeCore{}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	if !core.called("register") {
		t.Fatal("unknown user not registered")
	}
	mustContain(t, tg.last(t).text, "Подписки нет")
}

func TestPlanNamesAreEscaped(t *testing.T) {
	st := activeStatus()
	st.Plan.Name = "<b>VIP</b> & co"
	core := &fakeCore{status: st}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	mustContain(t, tg.last(t).text, "&lt;b&gt;VIP&lt;/b&gt; &amp; co")
}

func TestBuyListsPlansAndTheRules(t *testing.T) {
	core := &fakeCore{status: activeStatus(), plans: []Plan{
		{ID: 2, Name: "Месяц", PriceRub: 199, PeriodDays: 30, TrafficLimitBytes: i64(100 << 30), DeviceLimit: iptr(3), OnSale: true},
		{ID: 3, Name: "Год", PriceRub: 1990, PeriodDays: 365, OnSale: true},
	}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:buy"))
	m := tg.last(t)
	mustContain(t, m.text, "Месяц", "199 ₽", "30 дней", "100 ГБ", "до 3 устройств", "Год", "1990 ₽", "365 дней", "без лимита трафика",
		"<blockquote expandable>", "в очередь", "сгорит", "доступ закрыт до конца периода", "Оплатишь сейчас")
	if strings.Contains(m.text, "Если трафик кончился — новый период начнётся сразу") {
		t.Fatalf("misleading rule is back:\n%s", m.text)
	}
	b := buttons(m.keyboard)
	if b["buy:2"].Style != "success" {
		t.Fatalf("plan buttons are not green: %+v", m.keyboard)
	}
	if _, ok := b["buy:2"]; !ok {
		t.Fatalf("no button for plan 2: %+v", m.keyboard)
	}
	if _, ok := b["buy:3"]; !ok {
		t.Fatalf("no button for plan 3: %+v", m.keyboard)
	}
}

func TestBuyOffersToRenewAnOffSalePlan(t *testing.T) {
	st := activeStatus()
	st.Plan.OnSale = false
	core := &fakeCore{status: st, plans: []Plan{{ID: 3, Name: "Год", PriceRub: 1990, PeriodDays: 365, OnSale: true}}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:buy"))
	m := tg.last(t)
	if len(m.keyboard) == 0 || m.keyboard[0][0].CallbackData != "buy:2" || !strings.Contains(m.keyboard[0][0].Text, "Продлить текущий") {
		t.Fatalf("renewal is not the first button: %+v", m.keyboard)
	}
}

func TestBuyWithNothingOnSale(t *testing.T) {
	core := &fakeCore{status: &Status{User: User{ID: 7}}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:buy"))
	mustContain(t, tg.last(t).text, "тарифов в продаже нет")
}

func TestBuyCreatesAPaymentLink(t *testing.T) {
	core := &fakeCore{status: &Status{User: User{ID: 7}}, plans: []Plan{{ID: 2, Name: "Месяц", PriceRub: 199, PeriodDays: 30, OnSale: true}},
		payment: &Payment{PaymentID: "2d85d5ad-000f-5000-9000-1c4c6dd1d9e6", ConfirmationURL: "https://yoomoney.ru/checkout?orderId=1", AmountRub: 199}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("buy:2"))
	if len(core.created) != 1 || core.created[0] != [2]int64{7, 2} {
		t.Fatalf("created = %v", core.created)
	}
	m := tg.last(t)
	mustContain(t, m.text, "Месяц", "199 ₽")
	b := buttons(m.keyboard)
	if pay, ok := b["https://yoomoney.ru/checkout?orderId=1"]; !ok || !strings.Contains(pay.Text, "199") || pay.Style != "success" {
		t.Fatalf("no green pay button: %+v", m.keyboard)
	}
	mustContain(t, m.text, "30 дней", "Я оплатил")
	if _, ok := b["chk:2d85d5ad-000f-5000-9000-1c4c6dd1d9e6"]; !ok {
		t.Fatalf("no check button: %+v", m.keyboard)
	}
	for _, row := range m.keyboard {
		for _, btn := range row {
			if len(btn.CallbackData) > 64 {
				t.Fatalf("callback data longer than 64 bytes: %q", btn.CallbackData)
			}
		}
	}
}

func TestBuyWarnsWhatHappensToTheCurrentPeriod(t *testing.T) {
	plans := []Plan{{ID: 2, Name: "Месяц", PriceRub: 199, PeriodDays: 30, OnSale: true}, {ID: 3, Name: "Год", PriceRub: 1990, PeriodDays: 365, OnSale: true}}
	pay := &Payment{PaymentID: "p1", ConfirmationURL: "https://yoomoney.ru/x", AmountRub: 1990}
	core := &fakeCore{status: activeStatus(), plans: plans, payment: pay}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("buy:3"))
	mustContain(t, tg.last(t).text, "«Месяц»", "сгорит")
	app.Handle(context.Background(), tg, callback("buy:2"))
	mustContain(t, tg.last(t).text, "в очередь")
	core.status.Subscription.TrafficUsedBytes = 100 << 30
	for _, plan := range []string{"buy:3", "buy:2"} {
		app.Handle(context.Background(), tg, callback(plan))
		m := tg.last(t).text
		if strings.Contains(m, "сгорит") || strings.Contains(m, "в очередь") {
			t.Fatalf("%s: exhausted period neither burns nor queues:\n%s", plan, m)
		}
		mustContain(t, m, "Трафик исчерпан", "начнётся сразу после оплаты")
	}
}

func TestBuyUnknownPlan(t *testing.T) {
	core := &fakeCore{status: activeStatus(), plans: []Plan{{ID: 3, Name: "Год", PriceRub: 1990, PeriodDays: 365, OnSale: true}}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("buy:99"))
	if core.called("create") {
		t.Fatal("payment created for a plan that is not offered")
	}
	mustContain(t, tg.last(t).text, "больше не продаётся")
	app.Handle(context.Background(), tg, callback("buy:abc"))
	if core.called("create") {
		t.Fatal("payment created for garbage")
	}
}

func TestBuyErrorsTellTheUserWhatHappened(t *testing.T) {
	for err, want := range map[error]string{
		ErrConflict:            "больше не продаётся",
		ErrPaymentsDisabled:    "Оплата сейчас недоступна",
		ErrProviderUnavailable: "ЮKassa не отвечает",
		errors.New("boom"):     "Что-то пошло не так",
	} {
		core := &fakeCore{status: &Status{User: User{ID: 7}}, plans: []Plan{{ID: 2, Name: "Месяц", PriceRub: 199, PeriodDays: 30, OnSale: true}}, payErr: err}
		app, tg := newTestApp(t, core)
		app.Handle(context.Background(), tg, callback("buy:2"))
		answers := tg.only(t, "answerCallbackQuery")
		if len(answers) != 1 || !strings.Contains(answers[0].text, want) || !answers[0].alert {
			t.Fatalf("%v: answers = %+v", err, answers)
		}
	}
}

func TestCheckSucceededPayment(t *testing.T) {
	core := &fakeCore{check: &Check{Status: "succeeded", Applied: true, TelegramID: i64(tgUser), PaidUntil: tptr(at("2026-11-30T10:00:00Z"))}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("chk:p1"))
	if !core.called("check:p1") {
		t.Fatalf("calls = %v", core.calls)
	}
	m := tg.last(t)
	if m.method != "editMessageText" {
		t.Fatalf("message = %+v", m)
	}
	mustContain(t, m.text, "Оплата прошла", "30.11.2026 13:00")
}

func TestCheckSomeoneElsesPayment(t *testing.T) {
	core := &fakeCore{check: &Check{Status: "succeeded", Applied: true, TelegramID: i64(4242), PaidUntil: tptr(at("2026-11-30T10:00:00Z"))}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("chk:p1"))
	answers := tg.only(t, "answerCallbackQuery")
	if len(answers) != 1 || !strings.Contains(answers[0].text, "не найден") {
		t.Fatalf("answers = %+v", answers)
	}
	if len(tg.only(t, "editMessageText"))+len(tg.only(t, "sendMessage")) != 0 {
		t.Fatalf("foreign payment shown: %+v", tg.calls)
	}
	core.check, core.checkErr = nil, ErrNotFound
	app.Handle(context.Background(), tg, callback("chk:p2"))
	if a := tg.only(t, "answerCallbackQuery"); !strings.Contains(a[len(a)-1].text, "не найден") {
		t.Fatalf("answers = %+v", a)
	}
}

func TestCheckPendingCanceledAndMismatch(t *testing.T) {
	core := &fakeCore{check: &Check{Status: "pending", TelegramID: i64(tgUser)}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("chk:p1"))
	a := tg.only(t, "answerCallbackQuery")
	if len(a) != 1 || !a[0].alert || !strings.Contains(a[0].text, "ещё не пришла") || len(tg.only(t, "editMessageText")) != 0 {
		t.Fatalf("pending: %+v", tg.calls)
	}
	core.check = &Check{Status: "canceled", TelegramID: i64(tgUser)}
	app.Handle(context.Background(), tg, callback("chk:p1"))
	mustContain(t, tg.last(t).text, "отменён")
	core.check = &Check{Status: "mismatch", TelegramID: i64(tgUser)}
	app.Handle(context.Background(), tg, callback("chk:p1"))
	mustContain(t, tg.last(t).text, "что-то не так")
	core.check, core.checkErr = nil, ErrProviderUnavailable
	app.Handle(context.Background(), tg, callback("chk:p1"))
	if a := tg.only(t, "answerCallbackQuery"); !strings.Contains(a[len(a)-1].text, "ЮKassa не отвечает") {
		t.Fatalf("answers = %+v", a)
	}
}

func TestResetDevicesAsksFirst(t *testing.T) {
	core := &fakeCore{status: activeStatus()}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("dev:ask"))
	if core.called("reset") {
		t.Fatal("devices reset without confirmation")
	}
	if ok := buttons(tg.last(t).keyboard)["dev:ok"]; ok.Style != "danger" {
		t.Fatalf("no red confirm button: %+v", tg.last(t))
	}
	app.Handle(context.Background(), tg, callback("dev:ok"))
	if len(core.resetFor) != 1 || core.resetFor[0] != 7 {
		t.Fatalf("reset for = %v", core.resetFor)
	}
	mustContain(t, tg.last(t).text, "сброшены")
}

func TestRotateLinkAsksFirstAndShowsTheNewLink(t *testing.T) {
	core := &fakeCore{status: activeStatus(), rotated: "new-tok"}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("link:ask"))
	if core.called("rotate") {
		t.Fatal("link rotated without confirmation")
	}
	mustContain(t, tg.last(t).text, "перестанут работать")
	if ok := buttons(tg.last(t).keyboard)["link:ok"]; ok.Style != "danger" {
		t.Fatalf("no red confirm button: %+v", tg.last(t))
	}
	app.Handle(context.Background(), tg, callback("link:ok"))
	if len(core.rotateFor) != 1 || core.rotateFor[0] != 7 {
		t.Fatalf("rotate for = %v", core.rotateFor)
	}
	m := tg.last(t)
	mustContain(t, m.text, "<code>https://subs.example.com/new-tok</code>")
	if strings.Contains(m.text, "/tok<") {
		t.Fatalf("old link still shown:\n%s", m.text)
	}
}

func TestUnknownCallbackIsJustAnswered(t *testing.T) {
	core := &fakeCore{status: activeStatus()}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("whatever"))
	if len(tg.only(t, "answerCallbackQuery")) != 1 || len(core.calls) != 0 {
		t.Fatalf("calls = %+v / %v", tg.calls, core.calls)
	}
}

func TestCoreFailureOnStatusIsReported(t *testing.T) {
	core := &fakeCore{statusErr: errors.New("core down")}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	a := tg.only(t, "answerCallbackQuery")
	if len(a) != 1 || !a[0].alert || !strings.Contains(a[0].text, "Что-то пошло не так") {
		t.Fatalf("answers = %+v", a)
	}
}

func TestEditFailureFallsBackToANewMessage(t *testing.T) {
	core := &fakeCore{status: activeStatus()}
	app, tg := newTestApp(t, core)
	tg.editErr = errors.New("message can't be edited")
	app.Handle(context.Background(), tg, callback("menu:sub"))
	msgs := tg.only(t, "sendMessage")
	if len(msgs) != 1 || msgs[0].chatID != tgUser {
		t.Fatalf("fallback = %+v", msgs)
	}
	mustContain(t, msgs[0].text, "Месяц")
}

func TestNotModifiedEditIsNotResent(t *testing.T) {
	core := &fakeCore{status: activeStatus()}
	app, tg := newTestApp(t, core)
	tg.editErr = errors.New("bad request, Bad Request: message is not modified")
	app.Handle(context.Background(), tg, callback("menu:sub"))
	if len(tg.only(t, "sendMessage")) != 0 {
		t.Fatal("unchanged message sent again")
	}
}

func TestInaccessibleMessageGetsANewMessage(t *testing.T) {
	core := &fakeCore{status: activeStatus()}
	app, tg := newTestApp(t, core)
	cb := callback("menu:sub")
	cb.CallbackQuery.Message = models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeInaccessibleMessage, InaccessibleMessage: &models.InaccessibleMessage{Chat: models.Chat{ID: tgUser, Type: models.ChatTypePrivate}}}
	app.Handle(context.Background(), tg, cb)
	if len(tg.only(t, "sendMessage")) != 1 || len(tg.only(t, "editMessageText")) != 0 {
		t.Fatalf("calls = %+v", tg.calls)
	}
}

func TestNotifyPaid(t *testing.T) {
	app, tg := newTestApp(t, &fakeCore{})
	if err := app.NotifyPaid(context.Background(), tg, tgUser, tptr(at("2026-11-30T10:00:00Z"))); err != nil {
		t.Fatal(err)
	}
	msgs := tg.only(t, "sendMessage")
	if len(msgs) != 1 || msgs[0].chatID != tgUser {
		t.Fatalf("notify = %+v", msgs)
	}
	mustContain(t, msgs[0].text, "Оплата прошла", "30.11.2026 13:00")
}

func TestFormatBytes(t *testing.T) {
	for in, want := range map[int64]string{0: "0 МБ", 512 << 20: "512 МБ", 1 << 30: "1 ГБ", 1536 << 20: "1,5 ГБ", 100 << 30: "100 ГБ", 1<<40 + 1<<39: "1,5 ТБ"} {
		if got := formatBytes(in); got != want {
			t.Fatalf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckRefusesForgedPaymentIDs(t *testing.T) {
	core := &fakeCore{check: &Check{Status: "succeeded", TelegramID: i64(tgUser)}}
	app, tg := newTestApp(t, core)
	for _, id := range []string{"..", "../users/7/rotate", "", "a b"} {
		app.Handle(context.Background(), tg, callback("chk:"+id))
	}
	if len(core.calls) != 0 {
		t.Fatalf("forged ids reached the core: %v", core.calls)
	}
	for _, a := range tg.only(t, "answerCallbackQuery") {
		if !strings.Contains(a.text, "не найден") {
			t.Fatalf("answers = %+v", a)
		}
	}
}

func tgTime(tm time.Time) string {
	loc, _ := time.LoadLocation("Europe/Moscow")
	return fmt.Sprintf(`<tg-time unix="%d" format="Dt">%s</tg-time>`, tm.Unix(), tm.In(loc).Format("02.01.2006 15:04"))
}

func allTexts(t *testing.T, app *App, tg *fakeTelegram, core *fakeCore) []sent {
	t.Helper()
	core.plans = []Plan{{ID: 2, Name: "Месяц", PriceRub: 199, PeriodDays: 30, TrafficLimitBytes: i64(100 << 30), DeviceLimit: iptr(3), OnSale: true}, {ID: 3, Name: "Год", PriceRub: 1990, PeriodDays: 365, OnSale: true}}
	core.payment = &Payment{PaymentID: "p1", ConfirmationURL: "https://yoomoney.ru/x", AmountRub: 1990}
	core.check = &Check{Status: "succeeded", TelegramID: i64(tgUser), PaidUntil: tptr(at("2026-11-30T10:00:00Z"))}
	core.rotated = "new-tok"
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	for _, data := range []string{"menu:sub", "menu:buy", "buy:3", "chk:p1", "dev:ask", "dev:ok", "link:ask", "link:ok", "menu:main"} {
		app.Handle(context.Background(), tg, callback(data))
	}
	if err := app.NotifyPaid(context.Background(), tg, tgUser, tptr(at("2026-11-30T10:00:00Z"))); err != nil {
		t.Fatal(err)
	}
	var out []sent
	for _, c := range tg.calls {
		if c.method != "answerCallbackQuery" {
			out = append(out, c)
		}
	}
	return out
}

func TestScreensAreAiry(t *testing.T) {
	core := &fakeCore{status: activeStatus()}
	app, tg := newTestApp(t, core)
	for _, m := range allTexts(t, app, tg, core) {
		if !strings.Contains(m.text, rule+"\n\n") {
			t.Fatalf("no air after the header:\n%s", m.text)
		}
		if strings.Contains(m.text, "<tg-emoji") {
			t.Fatalf("custom emoji without the switch:\n%s", m.text)
		}
		for _, row := range m.keyboard {
			for _, b := range row {
				if b.IconCustomEmojiID != "" {
					t.Fatalf("custom button icon without the switch: %+v", b)
				}
			}
		}
	}
}

func TestCustomEmojiFromThePack(t *testing.T) {
	core := &fakeCore{status: activeStatus()}
	app, tg := newTestApp(t, core)
	app.UseCustomEmoji(true)
	screens := allTexts(t, app, tg, core)
	status := screens[1]
	mustContain(t, status.text, `<tg-emoji emoji-id="6030445631921721471">🛡</tg-emoji>`, `<tg-emoji emoji-id="5936143551854285132">📊</tg-emoji>`)
	b := buttons(status.keyboard)
	if buy := b["menu:buy"]; buy.IconCustomEmojiID != "6037083366438737901" || buy.Text != "Купить / продлить" {
		t.Fatalf("buy button = %+v", buy)
	}
	for _, m := range screens {
		if !strings.Contains(m.text, "<tg-emoji") {
			t.Fatalf("screen without pack icons:\n%s", m.text)
		}
		for _, row := range m.keyboard {
			for _, btn := range row {
				if btn.IconCustomEmojiID == "" {
					t.Fatalf("button without a pack icon: %+v", btn)
				}
				if r := []rune(btn.Text); len(r) > 0 && r[0] > 0x2000 {
					t.Fatalf("button keeps a plain emoji next to the pack icon: %q", btn.Text)
				}
			}
		}
	}
}
