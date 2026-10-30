package bot

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStatusShowsEveryPool(t *testing.T) {
	st := activeStatus()
	st.Subscription.Pools = []PoolUsage{{PoolID: 4, Name: "Германия <DE>", UsedBytes: 100 << 30, TrafficLimitBytes: i64(100 << 30), Exhausted: true},
		{PoolID: 5, Name: "CDN", UsedBytes: 3 << 30}}
	core := &fakeCore{status: st}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	m := tg.last(t).text
	mustContain(t, m, "✅ <b>Активна</b>", "Основной", "1 ГБ из 100 ГБ", "Германия &lt;DE&gt; · исчерпан", "100 ГБ из 100 ГБ", "CDN", "3 ГБ · без лимита")
	if strings.Contains(m, "Трафик исчерпан") {
		t.Fatalf("one spent pool must not read as the whole subscription:\n%s", m)
	}
	st.Subscription.TrafficExhausted = true
	app.Handle(context.Background(), tg, callback("menu:sub"))
	mustContain(t, tg.last(t).text, "Трафик исчерпан")
}

func TestPlanCardListsItsPools(t *testing.T) {
	app, _ := newTestApp(t, &fakeCore{})
	p := Plan{Name: "Премиум", PriceRub: 499, PeriodDays: 30, TrafficLimitBytes: i64(500 << 30),
		Pools: []PlanPool{{Label: "Германия", TrafficLimitBytes: i64(100 << 30)}, {Label: "CDN <x>"}}}
	got := app.planBlock(p)
	mustContain(t, got, "500 ГБ трафика", "Германия — 100 ГБ", "CDN &lt;x&gt; — без лимита")
	plain := app.planBlock(Plan{Name: "Месяц", PriceRub: 199, PeriodDays: 30, TrafficLimitBytes: i64(100 << 30)})
	if strings.Contains(plain, "—") {
		t.Fatalf("a plan without pools grew pool lines:\n%s", plain)
	}
}

func TestPoolNoticesNameThePool(t *testing.T) {
	app, _ := noticeApp(t)
	ends := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	pool := int64(4)
	text, _ := app.noticeView(Notice{Kind: "traffic_out", PoolID: &pool, PoolName: "Германия", MultiPool: true,
		TrafficUsedBytes: ip(100 * gb), TrafficLimitBytes: ip(100 * gb), PeriodEnds: &ends, PaidUntil: &ends})
	mustContain(t, text, "Трафик «Германия» закончился", "100 ГБ из 100 ГБ", "Остальные серверы работают")
	if strings.Contains(text, "VPN не работает") {
		t.Fatalf("a spent pool must not read as a dead VPN:\n%s", text)
	}
	text, _ = app.noticeView(Notice{Kind: "traffic_80", MultiPool: true, TrafficUsedBytes: ip(40 * gb), TrafficLimitBytes: ip(50 * gb), PeriodEnds: &ends, PaidUntil: &ends})
	mustContain(t, text, "Израсходовано 80% основного трафика")
	text, _ = app.noticeView(Notice{Kind: "traffic_out", PoolID: &pool, PoolName: "Германия", MultiPool: true, TrafficExhausted: true,
		TrafficUsedBytes: ip(100 * gb), TrafficLimitBytes: ip(100 * gb), PeriodEnds: &ends, PaidUntil: &ends})
	mustContain(t, text, "Трафик «Германия» закончился", "VPN не работает")
}

func TestStatusHidesAMainPoolThatCoversNothing(t *testing.T) {
	st := activeStatus()
	st.Plan.TrafficLimitBytes = nil
	st.Subscription.MainIdle = true
	st.Subscription.Pools = []PoolUsage{{PoolID: 4, Name: "Премиум", UsedBytes: 10 << 30, TrafficLimitBytes: i64(100 << 30)}}
	app, tg := newTestApp(t, &fakeCore{status: st})
	app.Handle(context.Background(), tg, callback("menu:sub"))
	m := tg.last(t).text
	mustContain(t, m, "Премиум", "10 ГБ из 100 ГБ")
	if strings.Contains(m, "Основной") || strings.Contains(m, "без лимита") {
		t.Fatalf("an idle main pool leaked into the status:\n%s", m)
	}
}
