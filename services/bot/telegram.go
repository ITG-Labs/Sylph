package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func NewTelegram(token, apiURL string, app *App, log *slog.Logger) (*tgbot.Bot, error) {
	b, err := tgbot.New(token,
		tgbot.WithServerURL(apiURL),
		tgbot.WithAllowedUpdates(tgbot.AllowedUpdates{"message", "callback_query"}),
		tgbot.WithDefaultHandler(safeHandler(app, log)),
		tgbot.WithErrorsHandler(telegramErrors(log, token)),
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

func telegramErrors(log *slog.Logger, token string) tgbot.ErrorsHandler {
	return func(err error) {
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
