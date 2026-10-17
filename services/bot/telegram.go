package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Conflicts struct {
	last atomic.Int64
}

func (c *Conflicts) record(t time.Time) {
	if c != nil {
		c.last.Store(t.UnixNano())
	}
}

func (c *Conflicts) Last() *time.Time {
	if c == nil {
		return nil
	}
	n := c.last.Load()
	if n == 0 {
		return nil
	}
	t := time.Unix(0, n).UTC()
	return &t
}

func NewTelegram(token, apiURL string, app *App, log *slog.Logger, conflicts *Conflicts) (*tgbot.Bot, error) {
	b, err := tgbot.New(token,
		tgbot.WithServerURL(apiURL),
		tgbot.WithAllowedUpdates(tgbot.AllowedUpdates{"message", "callback_query"}),
		tgbot.WithDefaultHandler(safeHandler(app, log)),
		tgbot.WithErrorsHandler(telegramErrors(log, token, conflicts)),
	)
	if err != nil {
		return nil, fmt.Errorf("telegram: %s", mask(err.Error(), token))
	}
	return b, nil
}

func safeHandler(app *App, log *slog.Logger) tgbot.HandlerFunc {
	return func(ctx context.Context, b *tgbot.Bot, u *models.Update) {
		defer func() {
			if r := recover(); r != nil {
				log.LogAttrs(ctx, slog.LevelError, "update_panic", slog.Int64("update_id", u.ID), slog.String("panic", fmt.Sprint(r)))
			}
		}()
		app.Handle(ctx, b, u)
	}
}

func telegramErrors(log *slog.Logger, token string, conflicts *Conflicts) tgbot.ErrorsHandler {
	return func(err error) {
		if errors.Is(err, tgbot.ErrorConflict) {
			conflicts.record(time.Now())
		}
		log.LogAttrs(context.Background(), slog.LevelError, "telegram_error", slog.String("error", mask(err.Error(), token)))
	}
}

func mask(s, token string) string {
	if token == "" {
		return s
	}
	s = strings.ReplaceAll(s, token, "***")
	if _, secret, ok := strings.Cut(token, ":"); ok && secret != "" {
		s = strings.ReplaceAll(s, secret, "***")
	}
	return s
}
