package bot

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStatusShowsEveryPool(t *testing.T) {
	st := activeStatus()
	st.Subscription.MainIdle, st.Subscription.TrafficUsedBytes = false, 1<<30
	st.Subscription.Pools = []PoolUsage{{PoolID: 4, Name: "Германия <DE>", UsedBytes: 100 << 30, TrafficLimitBytes: i64(100 << 30), Exhausted: true},
		{PoolID: 5, Name: "CDN", UsedBytes: 3 << 30}}
	core := &fakeCore{status: st}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	m := tg.last(t).text
	mustContain(t, m, "✅ <b>Активна</b>", "Германия &lt;DE&gt; · исчерпан", "100 ГБ из 100 ГБ", "CDN", "3 ГБ · без лимита", "Остальные серверы", "1 ГБ · без лимита")
	if strings.Contains(m, "Трафик исчерпан") {
		t.Fatalf("one spent pool must not read as the whole subscription:\n%s", m)
	}
	st.Subscription.TrafficExhausted = true
	app.Handle(context.Background(), tg, callback("menu:sub"))
	mustContain(t, tg.last(t).text, "Трафик исчерпан")
}

func TestPlanCardListsItsPools(t *testing.T) {
	app, _ := newTestApp(t, &fakeCore{})
	p := Plan{Name: "Премиум", PriceRub: 499, PeriodDays: 30, FreeInbounds: 2,
		Pools: []PlanPool{{Name: "Основной", TrafficLimitBytes: i64(500 << 30)}, {Name: "Германия", TrafficLimitBytes: i64(100 << 30)}, {Name: "CDN <x>"}}}
	got := app.planBlock(p)
	mustContain(t, got, "трафик по серверам:", "Основной — 500 ГБ", "Германия — 100 ГБ", "CDN &lt;x&gt; — без лимита", "Остальные серверы — без лимита")
	plain := app.planBlock(Plan{Name: "Месяц", PriceRub: 199, PeriodDays: 30, Pools: mainPool(100 << 30)})
	mustContain(t, plain, "100 ГБ трафика")
	if strings.Contains(plain, "—") || strings.Contains(plain, "Основной") {
		t.Fatalf("a plan with one pool over everything grew pool lines:\n%s", plain)
	}
	mustContain(t, app.planBlock(Plan{Name: "Безлимит", PriceRub: 1, PeriodDays: 30}), "без лимита трафика")
	mustContain(t, app.planBlock(Plan{Name: "Частично", PriceRub: 1, PeriodDays: 30, Pools: mainPool(1 << 30), FreeInbounds: 1}), "Основной — 1 ГБ", "Остальные серверы — без лимита")
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

func TestStatusShowsASinglePoolAsPlainTraffic(t *testing.T) {
	st := activeStatus()
	st.Subscription.Pools = []PoolUsage{{PoolID: 4, Name: "Премиум", UsedBytes: 10 << 30, TrafficLimitBytes: i64(100 << 30)}}
	app, tg := newTestApp(t, &fakeCore{status: st})
	app.Handle(context.Background(), tg, callback("menu:sub"))
	m := tg.last(t).text
	mustContain(t, m, "10 ГБ из 100 ГБ")
	if strings.Contains(m, "Премиум") || strings.Contains(m, "без лимита") || strings.Contains(m, "Остальные") {
		t.Fatalf("one pool over everything the plan sees must read as plain traffic:\n%s", m)
	}
}
