package bot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func gatedConfig() *BotConfig {
	return &BotConfig{
		Terms:   TermsConfig{Enabled: true, URL: "https://telegra.ph/terms", Version: 1},
		Channel: ChannelConfig{Enabled: true, Chat: "@nora_vpn", URL: "https://t.me/nora_vpn"},
		Help:    HelpConfig{Enabled: true, Text: "<b>Пиши в поддержку</b>", Buttons: []HelpButton{{Label: "Поддержка", URL: "https://t.me/nora_help"}, {Label: "Инструкции", URL: "https://telegra.ph/how"}}},
	}
}

func member() models.ChatMember {
	return models.ChatMember{Type: models.ChatMemberTypeMember, Member: &models.ChatMemberMember{}}
}

func TestNoGatesShowsMenu(t *testing.T) {
	core := &fakeCore{}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	m := tg.last(t)
	mustContain(t, m.text, "Привет")
	if _, ok := buttons(m.keyboard)[cbHelp]; ok {
		t.Fatal("help button without help configured")
	}
	if tg.memberN != 0 {
		t.Fatal("asked Telegram about a channel nobody configured")
	}
}

func TestTermsComeFirstAndCarryTheInvite(t *testing.T) {
	core := &fakeCore{config: gatedConfig(), redeem: &Redeem{Plan: Plan{ID: 5, Name: "Друзья", PriceRub: 99, PeriodDays: 30, OnSale: true}}}
	app, tg := newTestApp(t, core)
	tg.members = map[int64]models.ChatMember{tgUser: member()}
	app.Handle(context.Background(), tg, message("/start "+inviteCode, models.ChatTypePrivate))
	m := tg.last(t)
	mustContain(t, m.text, "Пользовательское соглашение", "Принимаю")
	b := buttons(m.keyboard)
	accept := cbTermsPref + "1:" + inviteCode
	if _, ok := b[accept]; !ok || urlButton(m.keyboard, "https://telegra.ph/terms") == nil || b[cbHelp].CallbackData == "" {
		t.Fatalf("terms keyboard = %+v", m.keyboard)
	}
	if len(core.work()) != 0 || tg.memberN != 0 {
		t.Fatalf("worked before the terms: %v, member checks %d", core.work(), tg.memberN)
	}
	app.Handle(context.Background(), tg, callback(accept))
	if !core.called("terms:7:1") || !core.called("redeem:7:"+inviteCode) {
		t.Fatalf("calls = %v", core.calls)
	}
	mustContain(t, tg.last(t).text, "Тебе открыт тариф", "Друзья")
}

func TestTermsChangedWhileReading(t *testing.T) {
	cfg := gatedConfig()
	cfg.Channel.Enabled = false
	core := &fakeCore{config: cfg, acceptErr: ErrConflict}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	core.mu.Lock()
	core.config.Terms.Version = 2
	core.mu.Unlock()
	app.Handle(context.Background(), tg, callback(cbTermsPref+"1"))
	answers := tg.only(t, "answerCallbackQuery")
	if a := answers[len(answers)-1]; !a.alert || !strings.Contains(a.text, "обновилось") {
		t.Fatalf("answer = %+v", a)
	}
	m := tg.last(t)
	if _, ok := buttons(m.keyboard)[cbTermsPref+"2"]; !ok {
		t.Fatalf("stale screen kept: %+v", m.keyboard)
	}
}

func TestUpdatedTermsAskAgain(t *testing.T) {
	cfg := gatedConfig()
	cfg.Terms.Version = 3
	cfg.Terms.Text = "Мы поменяли <b>правила возврата</b>"
	core := &fakeCore{config: cfg, terms: 2}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback(cbStatus))
	m := tg.last(t)
	mustContain(t, m.text, "Соглашение обновилось", "правила возврата")
	if core.called("status") {
		t.Fatal("status shown before the new terms were accepted")
	}
}

func TestChannelGate(t *testing.T) {
	cfg := gatedConfig()
	cfg.Terms.Enabled = false
	core := &fakeCore{config: cfg}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	m := tg.last(t)
	mustContain(t, m.text, "Подпишись на канал", "Я подписался")
	if urlButton(m.keyboard, "https://t.me/nora_vpn") == nil || tg.memberAt != "@nora_vpn" {
		t.Fatalf("channel screen = %+v, asked about %v", m.keyboard, tg.memberAt)
	}
	app.Handle(context.Background(), tg, callback(cbJoinedPref))
	answers := tg.only(t, "answerCallbackQuery")
	if a := answers[len(answers)-1]; !a.alert || !strings.Contains(a.text, "Не вижу подписки") {
		t.Fatalf("answer = %+v", a)
	}
	tg.members = map[int64]models.ChatMember{tgUser: member()}
	app.Handle(context.Background(), tg, callback(cbJoinedPref))
	mustContain(t, tg.last(t).text, "Привет")
	n := tg.memberN
	app.Handle(context.Background(), tg, callback(cbStatus))
	if tg.memberN != n {
		t.Fatal("membership was not cached")
	}
	app.Handle(context.Background(), tg, callback(cbJoinedPref))
	if tg.memberN != n+1 {
		t.Fatal("«Я подписался» must check again")
	}
}

func TestChannelStatuses(t *testing.T) {
	cfg := gatedConfig()
	cfg.Terms.Enabled = false
	for name, c := range map[string]struct {
		m  models.ChatMember
		in bool
	}{
		"owner":           {models.ChatMember{Type: models.ChatMemberTypeOwner, Owner: &models.ChatMemberOwner{}}, true},
		"admin":           {models.ChatMember{Type: models.ChatMemberTypeAdministrator, Administrator: &models.ChatMemberAdministrator{}}, true},
		"restricted in":   {models.ChatMember{Type: models.ChatMemberTypeRestricted, Restricted: &models.ChatMemberRestricted{IsMember: true}}, true},
		"restricted out":  {models.ChatMember{Type: models.ChatMemberTypeRestricted, Restricted: &models.ChatMemberRestricted{IsMember: false}}, false},
		"banned":          {models.ChatMember{Type: models.ChatMemberTypeBanned, Banned: &models.ChatMemberBanned{}}, false},
		"left":            {models.ChatMember{Type: models.ChatMemberTypeLeft, Left: &models.ChatMemberLeft{}}, false},
		"restricted, nil": {models.ChatMember{Type: models.ChatMemberTypeRestricted}, false},
	} {
		core := &fakeCore{config: cfg}
		app, tg := newTestApp(t, core)
		tg.members = map[int64]models.ChatMember{tgUser: c.m}
		app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
		if got := !strings.Contains(tg.last(t).text, "Подпишись на канал"); got != c.in {
			t.Fatalf("%s: let in = %v", name, got)
		}
	}
}

func TestChannelCheckFailsOpen(t *testing.T) {
	cfg := gatedConfig()
	cfg.Terms.Enabled = false
	core := &fakeCore{config: cfg}
	app, tg := newTestApp(t, core)
	tg.memberErr = errors.New("Bad Request: member list is inaccessible")
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	mustContain(t, tg.last(t).text, "Привет")
}

func TestPaymentCheckSkipsGates(t *testing.T) {
	core := &fakeCore{config: gatedConfig(), check: &Check{Status: "succeeded", Applied: true}}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback(cbChkPrefix+"pay-1"))
	if !core.called("check:pay-1") {
		t.Fatalf("payment check gated: %v", core.calls)
	}
}

func TestHelp(t *testing.T) {
	cfg := gatedConfig()
	core := &fakeCore{config: cfg}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback(cbHelp))
	m := tg.last(t)
	mustContain(t, m.text, "Помощь", "<b>Пиши в поддержку</b>")
	if urlButton(m.keyboard, "https://t.me/nora_help") == nil || urlButton(m.keyboard, "https://telegra.ph/how") == nil {
		t.Fatalf("help keyboard = %+v", m.keyboard)
	}
	if _, ok := buttons(m.keyboard)[cbMenu]; !ok {
		t.Fatal("no way back from help")
	}

	cfg.Terms.Enabled, cfg.Channel.Enabled = false, false
	core = &fakeCore{config: cfg}
	app, tg = newTestApp(t, core)
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	if _, ok := buttons(tg.last(t).keyboard)[cbHelp]; !ok {
		t.Fatal("menu has no help button")
	}

	core = &fakeCore{}
	app, tg = newTestApp(t, core)
	app.Handle(context.Background(), tg, callback(cbHelp))
	mustContain(t, tg.last(t).text, "Привет")
}

func TestConfigCacheAndFallback(t *testing.T) {
	core := &fakeCore{config: gatedConfig()}
	app, tg := newTestApp(t, core)
	for range 3 {
		app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	}
	n := 0
	for _, c := range core.calls {
		if c == "config" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("config fetched %d times", n)
	}
	app.configTTL = 0
	core.mu.Lock()
	core.configErr = errors.New("core down")
	core.mu.Unlock()
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	mustContain(t, tg.last(t).text, "Пользовательское соглашение")

	fresh := &fakeCore{configErr: errors.New("core down")}
	app, tg = newTestApp(t, fresh)
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	mustContain(t, tg.last(t).text, "Привет")
}

func TestMembershipCacheExpires(t *testing.T) {
	cfg := gatedConfig()
	cfg.Terms.Enabled = false
	core := &fakeCore{config: cfg}
	app, tg := newTestApp(t, core)
	tg.members = map[int64]models.ChatMember{tgUser: member()}
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	app.memberTTL = time.Nanosecond
	time.Sleep(time.Millisecond)
	delete(tg.members, tgUser)
	app.Handle(context.Background(), tg, message("/start", models.ChatTypePrivate))
	mustContain(t, tg.last(t).text, "Подпишись на канал")
}

func urlButton(kb [][]models.InlineKeyboardButton, url string) *models.InlineKeyboardButton {
	for _, row := range kb {
		for i := range row {
			if row[i].URL == url {
				return &row[i]
			}
		}
	}
	return nil
}
