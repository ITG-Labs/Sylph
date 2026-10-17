package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTelegramSendAndUpdates(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot123:secret/sendMessage":
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Errorf("decode: %v", err)
			}
			if _, err := w.Write([]byte(`{"ok":true,"result":{}}`)); err != nil {
				t.Error(err)
			}
		case "/bot123:secret/getUpdates":
			if r.URL.Query().Get("offset") != "7" {
				t.Errorf("offset = %q", r.URL.Query().Get("offset"))
			}
			if _, err := w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"message":{"date":1,"text":"/status","chat":{"id":111},"from":{"id":111}}}]}`)); err != nil {
				t.Error(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	tg := NewTelegram(srv.URL, "123:secret")
	if err := tg.Send(context.Background(), 111, "привет"); err != nil {
		t.Fatal(err)
	}
	if sent["chat_id"] != float64(111) || sent["text"] != "привет" || sent["disable_web_page_preview"] != true {
		t.Fatalf("sent = %v", sent)
	}
	ups, err := tg.Updates(context.Background(), 7, 0)
	if err != nil || len(ups) != 1 || ups[0].ID != 7 || ups[0].Message == nil || ups[0].Message.Text != "/status" || ups[0].Message.Chat.ID != 111 {
		t.Fatalf("updates = %+v, %v", ups, err)
	}
}

func TestTelegramErrorsHideTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		if _, err := w.Write([]byte(`{"ok":false,"description":"Forbidden: bot was blocked by the user"}`)); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	err := NewTelegram(srv.URL, "123:secret").Send(context.Background(), 1, "x")
	if err == nil || !strings.Contains(err.Error(), "blocked") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
	err = NewTelegram("http://127.0.0.1:1", "123:secret").Send(context.Background(), 1, "x")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("network err = %v", err)
	}
}
