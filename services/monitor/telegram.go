package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Telegram struct {
	base  string
	token string
	http  *http.Client
}

type Update struct {
	ID      int64    `json:"update_id"`
	Message *Message `json:"message"`
}

type Message struct {
	Date int64  `json:"date"`
	Text string `json:"text"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From *struct {
		ID int64 `json:"id"`
	} `json:"from"`
}

func NewTelegram(base, token string) *Telegram {
	return &Telegram{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: 70 * time.Second}}
}

func (t *Telegram) Send(ctx context.Context, chatID int64, text string) error {
	body, err := json.Marshal(map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true})
	if err != nil {
		return err
	}
	return t.call(ctx, http.MethodPost, "sendMessage", nil, body, nil)
}

func (t *Telegram) Updates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	q := url.Values{}
	q.Set("offset", strconv.FormatInt(offset, 10))
	q.Set("timeout", strconv.Itoa(timeoutSec))
	q.Set("allowed_updates", `["message"]`)
	var out []Update
	if err := t.call(ctx, http.MethodGet, "getUpdates", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (t *Telegram) call(ctx context.Context, method, name string, q url.Values, body []byte, result any) error {
	u := t.base + "/bot" + t.token + "/" + name
	if q != nil {
		u += "?" + q.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return t.clean(fmt.Errorf("telegram %s: %w", name, err))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return t.clean(fmt.Errorf("telegram %s: %w", name, err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return t.clean(fmt.Errorf("telegram %s: read: %w", name, err))
	}
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d, bad response", name, resp.StatusCode)
	}
	if !env.OK {
		return t.clean(fmt.Errorf("telegram %s: HTTP %d: %s", name, resp.StatusCode, env.Description))
	}
	if result != nil {
		if err := json.Unmarshal(env.Result, result); err != nil {
			return fmt.Errorf("telegram %s: decode result: %w", name, err)
		}
	}
	return nil
}

func (t *Telegram) clean(err error) error {
	msg := strings.ReplaceAll(err.Error(), t.token, "***")
	if _, secret, ok := strings.Cut(t.token, ":"); ok && secret != "" {
		msg = strings.ReplaceAll(msg, secret, "***")
	}
	return fmt.Errorf("%s", msg)
}
