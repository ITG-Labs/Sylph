package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type fakeTelegramAPI struct {
	mu       sync.Mutex
	token    string
	updates  [][]byte
	allowed  []string
	messages []map[string]string
	sentCh   chan struct{}
}

func (f *fakeTelegramAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prefix := "/bot" + f.token + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
		return
	}
	_ = r.ParseMultipartForm(1 << 20)
	method := strings.TrimPrefix(r.URL.Path, prefix)
	w.Header().Set("Content-Type", "application/json")
	switch method {
	case "getMe":
		io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"Sylph","username":"sylph_test_bot"}}`)
	case "getUpdates":
		f.mu.Lock()
		if v := r.FormValue("allowed_updates"); v != "" {
			_ = json.Unmarshal([]byte(v), &f.allowed)
		}
		var batch []byte
		if len(f.updates) > 0 {
			batch, f.updates = f.updates[0], f.updates[1:]
		}
		f.mu.Unlock()
		if batch == nil {
			select {
			case <-r.Context().Done():
			case <-time.After(200 * time.Millisecond):
			}
			io.WriteString(w, `{"ok":true,"result":[]}`)
			return
		}
		w.Write([]byte(`{"ok":true,"result":` + string(batch) + `}`))
	case "sendMessage":
		f.mu.Lock()
		f.messages = append(f.messages, map[string]string{"chat_id": r.FormValue("chat_id"), "text": r.FormValue("text"), "reply_markup": r.FormValue("reply_markup")})
		f.mu.Unlock()
		io.WriteString(w, `{"ok":true,"result":{"message_id":2,"date":0,"chat":{"id":1001,"type":"private"}}}`)
		select {
		case f.sentCh <- struct{}{}:
		default:
		}
	default:
		io.WriteString(w, `{"ok":true,"result":true}`)
	}
}

func TestTelegramLoopDispatchesUpdatesToTheApp(t *testing.T) {
	api := &fakeTelegramAPI{token: "123:secret-token", sentCh: make(chan struct{}, 1)}
	upd, _ := json.Marshal([]models.Update{{ID: 1, Message: &models.Message{ID: 10, Text: "/start", From: &models.User{ID: 1001}, Chat: models.Chat{ID: 1001, Type: models.ChatTypePrivate}}}})
	api.updates = [][]byte{upd}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	core := &fakeCore{}
	app := NewApp(core, "https://subs.example.com", time.UTC, log)
	b, err := NewTelegram("123:secret-token", srv.URL, app, log, &Conflicts{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Start(ctx); close(done) }()
	select {
	case <-api.sentCh:
	case <-time.After(5 * time.Second):
		t.Fatal("no reply to /start")
	}
	cancel()
	<-done
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.messages) != 1 || api.messages[0]["chat_id"] != "1001" || !strings.Contains(api.messages[0]["reply_markup"], "menu:sub") {
		t.Fatalf("messages = %+v", api.messages)
	}
	if strings.Join(api.allowed, ",") != "message,callback_query" {
		t.Fatalf("allowed updates = %v", api.allowed)
	}
	if !core.called("register") {
		t.Fatal("core not called")
	}
}

func TestNewTelegramFailsOnABadToken(t *testing.T) {
	srv := httptest.NewServer(&fakeTelegramAPI{token: "good:token"})
	t.Cleanup(srv.Close)
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	_, err := NewTelegram("123:wrong-secret", srv.URL, NewApp(&fakeCore{}, "https://s", time.UTC, log), log, &Conflicts{})
	if err == nil {
		t.Fatal("bad token accepted")
	}
	if strings.Contains(err.Error(), "wrong-secret") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestTelegramErrorsAreLoggedWithoutTheToken(t *testing.T) {
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	telegramErrors(log, "123:secret-token", &Conflicts{})(errors.New(`Post "https://api.telegram.org/bot123:secret-token/getUpdates": dial tcp: timeout`))
	if strings.Contains(logs.String(), "secret-token") || !strings.Contains(logs.String(), `"msg":"telegram_error"`) {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestTelegramConflictIsRecorded(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	c := &Conflicts{}
	h := telegramErrors(log, "123:secret-token", c)
	h(errors.New("error get updates, dial tcp: timeout"))
	if c.Last() != nil {
		t.Fatal("a network error was taken for a conflict")
	}
	before := time.Now()
	h(fmt.Errorf("error get updates, %w", fmt.Errorf("%w, Conflict: terminated by other getUpdates request", tgbot.ErrorConflict)))
	if last := c.Last(); last == nil || last.Before(before.Add(-time.Second)) {
		t.Fatalf("conflict = %v", last)
	}
	var none *Conflicts
	if none.Last() != nil {
		t.Fatal("nil tracker reported a conflict")
	}
}

func TestHandlerPanicDoesNotKillTheBot(t *testing.T) {
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	app := NewApp(nil, "https://s", time.UTC, log)
	h := safeHandler(app, log)
	h(context.Background(), nil, &models.Update{Message: &models.Message{Text: "/start", From: &models.User{ID: 1}, Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate}}})
	if !strings.Contains(logs.String(), `"msg":"update_panic"`) {
		t.Fatalf("logs = %s", logs.String())
	}
}
