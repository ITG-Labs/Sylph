package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type marked struct {
	kind, ref, outcome string
	userID             int64
}

type fakeNotices struct {
	mu      sync.Mutex
	due     []Notice
	dueErr  error
	markErr error
	marks   []marked
}

func (f *fakeNotices) DueNotices(context.Context) ([]Notice, error) {
	return f.due, f.dueErr
}

func (f *fakeNotices) MarkNotice(_ context.Context, n Notice, outcome string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return f.markErr
	}
	f.marks = append(f.marks, marked{kind: n.Kind, ref: n.Ref, outcome: outcome, userID: n.UserID})
	return nil
}

type noticeTG struct {
	fakeTelegram
	errs map[int64]error
}

func (f *noticeTG) SendMessage(ctx context.Context, p *tgbot.SendMessageParams) (*models.Message, error) {
	if err := f.errs[p.ChatID.(int64)]; err != nil {
		return nil, err
	}
	return f.fakeTelegram.SendMessage(ctx, p)
}

func noticeApp(t *testing.T) (*App, *bytes.Buffer) {
	t.Helper()
	app, _ := newTestApp(t, &fakeCore{})
	logs := &bytes.Buffer{}
	app.log = slog.New(slog.NewJSONHandler(logs, nil))
	app.noticePause = 0
	return app, logs
}

func ip(v int64) *int64 { return &v }

const gb = int64(1) << 30

func TestNoticeTexts(t *testing.T) {
	app, _ := noticeApp(t)
	ends := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	later := ends.AddDate(0, 1, 0)
	cases := []struct {
		n    Notice
		want []string
		not  []string
	}{
		{Notice{Kind: "traffic_80", TrafficUsedBytes: ip(40 * gb), TrafficLimitBytes: ip(50 * gb), PeriodEnds: &ends, PaidUntil: &ends},
			[]string{"📊 <b>Израсходовано 80% трафика</b>", "40 ГБ из 50 ГБ", "▰▰▰▰▰▰▰▰▱▱", "Текущий период до", "12.11.2026 18:00"}, nil},
		{Notice{Kind: "traffic_95", TrafficUsedBytes: ip(48 * gb), TrafficLimitBytes: ip(50 * gb), PeriodEnds: &ends, PaidUntil: &ends},
			[]string{"Трафик почти закончился", "48 ГБ из 50 ГБ", "Текущий период до"}, nil},
		{Notice{Kind: "traffic_out", TrafficUsedBytes: ip(50 * gb), TrafficLimitBytes: ip(50 * gb), PeriodEnds: &ends, PaidUntil: &ends},
			[]string{"Трафик закончился", "трафик на этот период исчерпан, период — до", "12.11.2026 18:00", "новый период начнётся сразу, с полным трафиком"}, []string{"Следующий оплаченный", "Доступ закрыт"}},
		{Notice{Kind: "traffic_out", TrafficUsedBytes: ip(50 * gb), TrafficLimitBytes: ip(50 * gb), PeriodEnds: &ends, PaidUntil: &later},
			[]string{"Следующий оплаченный период начнётся", "12.11.2026 18:00", "Не хочешь ждать"}, []string{"исчерпан"}},
		{Notice{Kind: "expiry_3d", PaidUntil: &ends}, []string{"Подписка заканчивается", "Осталось меньше 3 дней", "Оплачено до", "12.11.2026 18:00", "Продли"}, nil},
		{Notice{Kind: "expiry_1d", PaidUntil: &ends}, []string{"Подписка скоро закончится", "Осталось меньше суток", "Оплачено до"}, []string{"Пробный"}},
		{Notice{Kind: "expiry_1h", PaidUntil: &ends}, []string{"Подписка закончится через час", "Оплачено до", "12.11.2026 18:00"}, []string{"меньше суток"}},
		{Notice{Kind: "expired"}, []string{"Подписка закончилась", "VPN не работает"}, []string{"Пробный"}},
		{Notice{Kind: "expiry_1d", Trial: true, PaidUntil: &ends}, []string{"🎁 <b>Пробный период скоро закончится</b>", "Пробный период до", "выбери тариф"}, []string{"Оплачено до", "Продли"}},
		{Notice{Kind: "expiry_1h", Trial: true, PaidUntil: &ends}, []string{"Пробный период закончится через час", "Пробный период до"}, []string{"Продли"}},
		{Notice{Kind: "expired", Trial: true}, []string{"Пробный период закончился", "Выбери тариф"}, []string{"VPN не работает"}},
	}
	for _, c := range cases {
		text, kb := app.noticeView(c.n)
		for _, w := range c.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s trial=%v: no %q in\n%s", c.n.Kind, c.n.Trial, w, text)
			}
		}
		for _, w := range c.not {
			if strings.Contains(text, w) {
				t.Errorf("%s trial=%v: unexpected %q in\n%s", c.n.Kind, c.n.Trial, w, text)
			}
		}
		if kb == nil || len(kb.InlineKeyboard) != 2 || kb.InlineKeyboard[0][0].CallbackData != cbBuy || kb.InlineKeyboard[1][0].CallbackData != cbStatus {
			t.Errorf("%s keyboard = %+v", c.n.Kind, kb)
		}
	}
	if text, kb := app.noticeView(Notice{Kind: "traffic_50"}); text != "" || kb != nil {
		t.Fatalf("unknown kind = %q", text)
	}
}

func TestNoticeTextsUseCustomEmoji(t *testing.T) {
	app, _ := noticeApp(t)
	app.UseCustomEmoji(true)
	text, kb := app.noticeView(Notice{Kind: "expired"})
	if !strings.Contains(text, `<tg-emoji emoji-id="`+icCross.id+`">`) || kb.InlineKeyboard[0][0].IconCustomEmojiID != icDiamond.id {
		t.Fatalf("custom emoji: %s", text)
	}
}

func TestSendNoticesMarksOutcomes(t *testing.T) {
	app, logs := noticeApp(t)
	until := time.Now().Add(time.Hour)
	core := &fakeNotices{due: []Notice{
		{UserID: 1, TelegramID: 11, Kind: "expiry_1h", Ref: "r1", PaidUntil: &until},
		{UserID: 2, TelegramID: 22, Kind: "expired", Ref: "r2"},
		{UserID: 3, TelegramID: 33, Kind: "expired", Ref: "r3"},
		{UserID: 4, TelegramID: 44, Kind: "expired", Ref: "r4"},
		{UserID: 5, TelegramID: 55, Kind: "traffic_50", Ref: "r5"},
		{UserID: 6, TelegramID: 66, Kind: "expired", Ref: "r6"},
	}}
	tg := &noticeTG{errs: map[int64]error{
		22: fmt.Errorf("%w, Forbidden: bot was blocked by the user", tgbot.ErrorForbidden),
		33: fmt.Errorf("%w, Bad Request: chat not found", tgbot.ErrorBadRequest),
		44: fmt.Errorf("%w, Bad Request: can't parse entities", tgbot.ErrorBadRequest),
	}}
	app.SendNotices(context.Background(), core, tg)
	want := []marked{{"expiry_1h", "r1", "sent", 1}, {"expired", "r2", "unreachable", 2}, {"expired", "r3", "unreachable", 3}, {"expired", "r4", "failed", 4}, {"expired", "r6", "sent", 6}}
	if fmt.Sprint(core.marks) != fmt.Sprint(want) {
		t.Fatalf("marks = %+v", core.marks)
	}
	sent := tg.only(t, "sendMessage")
	if len(sent) != 2 || sent[0].chatID != int64(11) || !sent[0].html || sent[1].chatID != int64(66) {
		t.Fatalf("sent = %+v", sent)
	}
	for _, msg := range []string{`"msg":"notice_send_failed"`, `"msg":"notice_unknown_kind"`, `"msg":"notice_sent"`} {
		if !strings.Contains(logs.String(), msg) {
			t.Errorf("no %s in logs", msg)
		}
	}
}

func TestSendNoticesStopsOnThrottle(t *testing.T) {
	app, logs := noticeApp(t)
	core := &fakeNotices{due: []Notice{
		{UserID: 1, TelegramID: 11, Kind: "expired", Ref: "r1"},
		{UserID: 2, TelegramID: 22, Kind: "expired", Ref: "r2"},
		{UserID: 3, TelegramID: 33, Kind: "expired", Ref: "r3"},
	}}
	tg := &noticeTG{errs: map[int64]error{22: &tgbot.TooManyRequestsError{Message: "too many requests", RetryAfter: 5}}}
	app.SendNotices(context.Background(), core, tg)
	if len(core.marks) != 1 || core.marks[0].ref != "r1" || len(tg.only(t, "sendMessage")) != 1 {
		t.Fatalf("marks = %+v", core.marks)
	}
	if !strings.Contains(logs.String(), `"msg":"notices_throttled"`) {
		t.Fatalf("logs = %s", logs)
	}
}

func TestSendNoticesCoreErrors(t *testing.T) {
	app, logs := noticeApp(t)
	tg := &noticeTG{}
	app.SendNotices(context.Background(), &fakeNotices{dueErr: fmt.Errorf("%w: api 404", ErrNotFound)}, tg)
	app.SendNotices(context.Background(), &fakeNotices{dueErr: errors.New("dial tcp: refused")}, tg)
	if len(tg.calls) != 0 || !strings.Contains(logs.String(), `"msg":"notices_unsupported"`) || !strings.Contains(logs.String(), `"msg":"notices_fetch_failed"`) {
		t.Fatalf("logs = %s", logs)
	}
	core := &fakeNotices{due: []Notice{{UserID: 1, TelegramID: 11, Kind: "expired", Ref: "r1"}, {UserID: 2, TelegramID: 22, Kind: "expired", Ref: "r2"}}, markErr: errors.New("core down")}
	app.SendNotices(context.Background(), core, tg)
	if len(tg.only(t, "sendMessage")) != 2 || strings.Count(logs.String(), `"msg":"notice_mark_failed"`) != 2 {
		t.Fatalf("mark failures: %s", logs)
	}
}

func TestSendNoticesPausesAndStopsOnCancel(t *testing.T) {
	app, _ := noticeApp(t)
	app.noticePause = time.Hour
	core := &fakeNotices{due: []Notice{{UserID: 1, TelegramID: 11, Kind: "expired", Ref: "r1"}, {UserID: 2, TelegramID: 22, Kind: "expired", Ref: "r2"}}}
	tg := &noticeTG{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		app.SendNotices(ctx, core, tg)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		core.mu.Lock()
		n := len(core.marks)
		core.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SendNotices did not stop on cancel")
	}
	if len(core.marks) != 1 {
		t.Fatalf("marks = %+v", core.marks)
	}
}

func TestCoreNotices(t *testing.T) {
	c, calls := fakeCoreClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `[{"user_id":7,"telegram_id":1001,"kind":"traffic_95","ref":"3:100","trial":false,"paid_until":"2026-11-12T15:00:00Z","period_ends":"2026-11-12T15:00:00Z","traffic_used_bytes":96,"traffic_limit_bytes":100}]`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	list, err := c.DueNotices(context.Background())
	if err != nil || len(list) != 1 || list[0].UserID != 7 || list[0].TelegramID != 1001 || list[0].Kind != "traffic_95" || list[0].Ref != "3:100" ||
		*list[0].TrafficUsedBytes != 96 || *list[0].TrafficLimitBytes != 100 || list[0].PeriodEnds == nil || list[0].PaidUntil == nil {
		t.Fatalf("due = %+v, %v", list, err)
	}
	if err := c.MarkNotice(context.Background(), list[0], outcomeUnreachable); err != nil {
		t.Fatal(err)
	}
	get, post := (*calls)[0], (*calls)[1]
	if get.method != http.MethodGet || get.path != "/v1/notifications/due" {
		t.Fatalf("get = %+v", get)
	}
	if post.method != http.MethodPost || post.path != "/v1/notifications/sent" || post.body["user_id"] != float64(7) || post.body["kind"] != "traffic_95" ||
		post.body["ref"] != "3:100" || post.body["outcome"] != "unreachable" || len(post.body) != 4 {
		t.Fatalf("post = %+v", post)
	}
}

func TestSendNoticesStopsWhenTheBotIsBroken(t *testing.T) {
	for _, broken := range []error{
		fmt.Errorf("%w, Unauthorized", tgbot.ErrorUnauthorized),
		fmt.Errorf("%w, Not Found", tgbot.ErrorNotFound),
	} {
		app, logs := noticeApp(t)
		core := &fakeNotices{due: []Notice{{UserID: 1, TelegramID: 11, Kind: "expired", Ref: "r1"}, {UserID: 2, TelegramID: 22, Kind: "expired", Ref: "r2"}}}
		tg := &noticeTG{errs: map[int64]error{11: broken, 22: broken}}
		app.SendNotices(context.Background(), core, tg)
		if len(core.marks) != 0 || strings.Count(logs.String(), `"msg":"notices_bot_broken"`) != 1 {
			t.Fatalf("%v: marks = %+v, logs = %s", broken, core.marks, logs)
		}
	}
}

type cancelingTG struct {
	noticeTG
	cancel context.CancelFunc
}

func (f *cancelingTG) SendMessage(ctx context.Context, p *tgbot.SendMessageParams) (*models.Message, error) {
	m, err := f.noticeTG.SendMessage(ctx, p)
	f.cancel()
	return m, err
}

type ctxCheckingNotices struct {
	fakeNotices
}

func (f *ctxCheckingNotices) MarkNotice(ctx context.Context, n Notice, outcome string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("mark without a deadline")
	}
	return f.fakeNotices.MarkNotice(ctx, n, outcome)
}

func TestSendNoticesMarksWhatWentOutDuringShutdown(t *testing.T) {
	app, _ := noticeApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	core := &ctxCheckingNotices{fakeNotices{due: []Notice{{UserID: 1, TelegramID: 11, Kind: "expired", Ref: "r1"}, {UserID: 2, TelegramID: 22, Kind: "expired", Ref: "r2"}}}}
	tg := &cancelingTG{cancel: cancel}
	app.SendNotices(ctx, core, tg)
	if len(core.marks) != 1 || core.marks[0].ref != "r1" || core.marks[0].outcome != "sent" || len(tg.only(t, "sendMessage")) != 1 {
		t.Fatalf("marks = %+v", core.marks)
	}
}

type dateStrictTG struct {
	fakeTelegram
	rejected int
}

func (f *dateStrictTG) reject(text string) error {
	if strings.Contains(text, "<tg-time") {
		f.rejected++
		return fmt.Errorf("%w, Bad Request: ENTITY_DATE_INVALID", tgbot.ErrorBadRequest)
	}
	return nil
}

func (f *dateStrictTG) SendMessage(ctx context.Context, p *tgbot.SendMessageParams) (*models.Message, error) {
	if err := f.reject(p.Text); err != nil {
		return nil, err
	}
	return f.fakeTelegram.SendMessage(ctx, p)
}

func (f *dateStrictTG) EditMessageText(ctx context.Context, p *tgbot.EditMessageTextParams) (*models.Message, error) {
	if err := f.reject(p.Text); err != nil {
		return nil, err
	}
	return f.fakeTelegram.EditMessageText(ctx, p)
}

func TestRejectedDatesFallBackToPlainText(t *testing.T) {
	app, _ := noticeApp(t)
	far := time.Date(2036, 10, 24, 9, 0, 0, 0, time.UTC)
	tg := &dateStrictTG{}
	app.show(context.Background(), tg, reply{chatID: 7, msgID: 3}, "Оплачено до "+app.when(far), nil)
	edits := tg.only(t, "editMessageText")
	if len(edits) != 1 || edits[0].text != "Оплачено до 24.10.2036 12:00" || tg.rejected != 1 {
		t.Fatalf("edit = %+v, rejected %d", edits, tg.rejected)
	}
	if err := app.NotifyPaid(context.Background(), tg, 7, &far); err != nil || strings.Contains(tg.last(t).text, "<tg-time") {
		t.Fatalf("paid = %v, %q", err, tg.last(t).text)
	}
	core := &fakeNotices{due: []Notice{{UserID: 1, TelegramID: 11, Kind: "expiry_3d", Ref: "r1", PaidUntil: &far}}}
	app.SendNotices(context.Background(), core, tg)
	if len(core.marks) != 1 || core.marks[0].outcome != "sent" || !strings.Contains(tg.last(t).text, "24.10.2036 12:00") {
		t.Fatalf("notice marks = %+v, text %q", core.marks, tg.last(t).text)
	}
	if got := plainDates("a " + app.when(far) + " b"); got != "a 24.10.2036 12:00 b" {
		t.Fatalf("plainDates = %q", got)
	}
}
