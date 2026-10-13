package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestBannedUserGetsNoAnswerAtAll(t *testing.T) {
	core := &fakeCore{registerErr: fmt.Errorf("%w: api 403 banned", ErrBanned), status: activeStatus()}
	app, tg := newTestApp(t, core)
	ctx := context.Background()
	app.Handle(ctx, tg, message("/start", models.ChatTypePrivate))
	app.Handle(ctx, tg, message("/start AAAAAAAAAAAAAAAAAAAAAA", models.ChatTypePrivate))
	for _, data := range []string{"menu:sub", "menu:buy", "link:ask", "dev:ask", "menu"} {
		app.Handle(ctx, tg, callback(data))
	}
	if len(tg.calls) != 0 {
		t.Fatalf("the bot answered a banned user: %+v", tg.calls)
	}
	for _, c := range core.calls {
		if c != "register" {
			t.Fatalf("the bot went further than the ban check: %v", core.calls)
		}
	}
}

func TestStatusCountsTheTrafficBonus(t *testing.T) {
	st := activeStatus()
	st.Subscription.TrafficUsedBytes = 100 << 30
	st.Subscription.TrafficLimitBytes = i64(150 << 30)
	core := &fakeCore{status: st}
	app, tg := newTestApp(t, core)
	app.Handle(context.Background(), tg, callback("menu:sub"))
	m := tg.last(t).text
	mustContain(t, m, "из 150")
	if strings.Contains(m, "Трафик исчерпан") {
		t.Fatalf("bonus ignored:\n%s", m)
	}
}

func TestWebhookStaysQuietForABannedUser(t *testing.T) {
	e := newWebhookEnv(t)
	e.core.checkErr = fmt.Errorf("%w: api 403 banned", ErrBanned)
	w := e.post("185.71.76.10:5555", "", notification("payment.succeeded", paymentID))
	if w.Code != http.StatusOK || len(e.notes) != 0 {
		t.Fatalf("webhook for a banned user: %d, notes %+v", w.Code, e.notes)
	}
}

func TestCoreMapsBannedToItsOwnError(t *testing.T) {
	c, _ := fakeCoreClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"code":"banned","message":"user is banned"}`)
	})
	if _, err := c.Register(context.Background(), 1); !errors.Is(err, ErrBanned) {
		t.Fatalf("register: %v", err)
	}
	f, _ := fakeCoreClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"code":"forbidden","message":"role not allowed"}`)
	})
	if _, err := f.Register(context.Background(), 1); errors.Is(err, ErrBanned) {
		t.Fatal("a forbidden role was read as a ban")
	}
}
