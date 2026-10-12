package bot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
)

func trialPlan() *Plan {
	return &Plan{ID: 9, Name: "Пробный", PeriodDays: 3, TrafficLimitBytes: i64(5 << 30), DeviceLimit: iptr(1), HWIDRequired: true, Trial: true, OnSale: true}
}

func TestMenuOffersTrialOnlyWhenAvailable(t *testing.T) {
	core := &fakeCore{status: &Status{User: User{ID: 7, SubToken: "tok"}, Trial: trialPlan()}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	m := tg.last(t)
	b := buttons(m.keyboard)
	if b["trial:ask"].Style != "success" || !strings.Contains(b["trial:ask"].Text, "Попробовать бесплатно") {
		t.Fatalf("no trial button: %+v", m.keyboard)
	}
	if _, ok := b["stats:"]; !ok {
		t.Fatalf("no stats button: %+v", m.keyboard)
	}
	core.status.Trial = nil
	app.Handle(context.Background(), tg, callback("menu:main"))
	m = tg.last(t)
	if _, ok := buttons(m.keyboard)["trial:ask"]; ok {
		t.Fatalf("trial offered without a trial plan: %+v", m.keyboard)
	}
	core.statusErr = fmt.Errorf("core down")
	app.Handle(context.Background(), tg, callback("menu:main"))
	if m := tg.last(t); len(m.keyboard) == 0 || strings.Contains(m.text, "trial") {
		t.Fatalf("menu without status = %+v", m)
	}
}

func TestTrialCardAndActivation(t *testing.T) {
	core := &fakeCore{status: &Status{User: User{ID: 7, SubToken: "tok"}, Trial: trialPlan()}, trialUntil: at("2026-10-16T10:00:00Z")}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("trial:ask"))
	m := tg.last(t)
	mustContain(t, m.text, "Пробный период", "3 дня", "5 ГБ", "Happ", "v2RayTun", "один раз")
	if b := buttons(m.keyboard); b["trial:ok"].Style != "success" {
		t.Fatalf("no activation button: %+v", m.keyboard)
	}
	app.Handle(context.Background(), tg, callback("trial:ok"))
	if !core.called("trial:7") {
		t.Fatalf("core calls = %v", core.calls)
	}
	m = tg.last(t)
	mustContain(t, m.text, "Пробный период включён", tgTime(at("2026-10-16T10:00:00Z")), "<code>https://subs.example.com/tok</code>")
	if _, ok := buttons(m.keyboard)["https://subs.example.com/tok"]; !ok {
		t.Fatalf("no open-link button: %+v", m.keyboard)
	}
}

func TestTrialWithoutHWIDSaysNothingAboutClients(t *testing.T) {
	p := trialPlan()
	p.HWIDRequired, p.DeviceLimit = false, nil
	core := &fakeCore{status: &Status{User: User{ID: 7, SubToken: "tok"}, Trial: p}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("trial:ask"))
	if m := tg.last(t); strings.Contains(m.text, "Happ") {
		t.Fatalf("hwid note on a trial without hwid:\n%s", m.text)
	}
}

func TestTrialUnavailable(t *testing.T) {
	core := &fakeCore{status: &Status{User: User{ID: 7, SubToken: "tok"}}, trialErr: ErrConflict}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("trial:ask"))
	answers := tg.only(t, "answerCallbackQuery")
	if len(answers) != 1 || !answers[0].alert || !strings.Contains(answers[0].text, "недоступен") {
		t.Fatalf("answers = %+v", answers)
	}
	core.status.Trial = trialPlan()
	app.Handle(context.Background(), tg, callback("trial:ok"))
	answers = tg.only(t, "answerCallbackQuery")
	if !answers[1].alert || !strings.Contains(answers[1].text, "недоступен") {
		t.Fatalf("answers = %+v", answers)
	}
}

func statsFixture() *Stats {
	return &Stats{PeriodID: i64(3), Nodes: []NodeStats{
		{NodeID: 1, Name: "Германия", Today: 0, Days7: 2 << 30, Days30: 3 << 30, Period: 1 << 30},
		{NodeID: 2, Name: "<b>NL</b>", Today: 5 << 20, Days7: 5 << 20, Days30: 5 << 20, Period: 2 << 30},
	}}
}

func TestStatsByServerWithTabs(t *testing.T) {
	core := &fakeCore{status: activeStatus(), stats: statsFixture()}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("stats:"))
	if !core.called("stats:7") {
		t.Fatalf("core calls = %v", core.calls)
	}
	m := tg.last(t)
	mustContain(t, m.text, "Статистика", "текущий период", "&lt;b&gt;NL&lt;/b&gt;", "2 ГБ", "Германия", "1 ГБ", "Всего", "3 ГБ")
	if strings.Index(m.text, "NL") > strings.Index(m.text, "Германия") {
		t.Fatalf("servers are not sorted by traffic:\n%s", m.text)
	}
	b := buttons(m.keyboard)
	for _, data := range []string{"stats:today", "stats:7", "stats:30", "stats:period"} {
		if _, ok := b[data]; !ok {
			t.Fatalf("no tab %s: %+v", data, m.keyboard)
		}
	}
	if b["stats:period"].Style != "primary" || b["stats:today"].Style != "" {
		t.Fatalf("selected tab not highlighted: %+v", m.keyboard)
	}
	app.Handle(context.Background(), tg, callback("stats:today"))
	m = tg.last(t)
	mustContain(t, m.text, "Сегодня", "5 МБ")
	if strings.Contains(m.text, "Германия") {
		t.Fatalf("server without traffic today is listed:\n%s", m.text)
	}
}

func TestStatsWithoutPeriodOrTraffic(t *testing.T) {
	core := &fakeCore{status: &Status{User: User{ID: 7, SubToken: "tok"}}, stats: &Stats{}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("stats:"))
	m := tg.last(t)
	mustContain(t, m.text, "За 30 дней", "Пока нет трафика")
	b := buttons(m.keyboard)
	if _, ok := b["stats:period"]; ok {
		t.Fatalf("period tab without a period: %+v", m.keyboard)
	}
	if b["stats:30"].Style != "primary" {
		t.Fatalf("30 days not selected: %+v", m.keyboard)
	}
	app.Handle(context.Background(), tg, callback("stats:bogus"))
	if m := tg.last(t); !strings.Contains(m.text, "За 30 дней") {
		t.Fatalf("unknown tab:\n%s", m.text)
	}
}

const inviteCode = "Ab3_-AAAAAAAAAAAAAAAAA"

func TestStartWithInviteOpensThePrivatePlan(t *testing.T) {
	core := &fakeCore{redeem: &Redeem{Plan: Plan{ID: 5, Name: "Друзья", PriceRub: 99, PeriodDays: 30, OnSale: true, Visibility: "private"}}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, message("/start "+inviteCode, models.ChatTypePrivate))
	if !core.called("register") || !core.called("redeem:7:"+inviteCode) {
		t.Fatalf("core calls = %v", core.calls)
	}
	m := tg.last(t)
	mustContain(t, m.text, "Тебе открыт тариф", "Друзья", "99 ₽", "закрытый")
	if b := buttons(m.keyboard); b["buy:5"].Style != "success" {
		t.Fatalf("no buy button: %+v", m.keyboard)
	}
}

func TestStartWithBadOrDeadInvite(t *testing.T) {
	core := &fakeCore{redeemErr: ErrConflict}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, message("/start hello", models.ChatTypePrivate))
	for _, c := range core.calls {
		if strings.HasPrefix(c, "redeem:") {
			t.Fatalf("bad payload sent to core: %v", core.calls)
		}
	}
	if m := tg.last(t); !strings.Contains(m.text, "Привет") {
		t.Fatalf("bad payload did not show the menu:\n%s", m.text)
	}
	app.Handle(context.Background(), tg, message("/start "+inviteCode, models.ChatTypePrivate))
	m := tg.last(t)
	mustContain(t, m.text, "Приглашение не сработало")
	if _, ok := buttons(m.keyboard)["menu:buy"]; !ok {
		t.Fatalf("dead invite leaves the user stuck: %+v", m.keyboard)
	}
}

func TestStatusShowsTheTelegramID(t *testing.T) {
	st := activeStatus()
	st.User.TelegramID = tgUser
	core := &fakeCore{status: st}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	mustContain(t, tg.last(t).text, "Твой Telegram ID", fmt.Sprintf("<code>%d</code>", tgUser))
}

func TestPrivatePlansAreMarked(t *testing.T) {
	core := &fakeCore{status: activeStatus(), plans: []Plan{{ID: 5, Name: "Друзья", PriceRub: 99, PeriodDays: 30, OnSale: true, Visibility: "private"}}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:buy"))
	if !core.called("plans:7") {
		t.Fatalf("plans not asked for the user: %v", core.calls)
	}
	mustContain(t, tg.last(t).text, "закрытый")
}
