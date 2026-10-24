package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func boolPtr(b bool) *bool { return &b }

func buttonsPtr(b ...store.HelpButton) *[]store.HelpButton { return &b }

func TestBotSettingsValidation(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	for _, c := range []struct {
		patch SettingsPatch
		field string
	}{
		{SettingsPatch{TermsEnabled: boolPtr(true)}, "terms_url"},
		{SettingsPatch{TermsURL: strPtr("http://telegra.ph/x")}, "terms_url"},
		{SettingsPatch{TermsURL: strPtr("telegra.ph/x")}, "terms_url"},
		{SettingsPatch{TermsText: strPtr("<b>не закрыт")}, "terms_text"},
		{SettingsPatch{TermsText: strPtr(strings.Repeat("я", 1001))}, "terms_text"},
		{SettingsPatch{ChannelEnabled: boolPtr(true)}, "channel_chat"},
		{SettingsPatch{ChannelChat: strPtr("nora")}, "channel_chat"},
		{SettingsPatch{ChannelChat: strPtr("@no")}, "channel_chat"},
		{SettingsPatch{ChannelChat: strPtr("@1nora")}, "channel_chat"},
		{SettingsPatch{ChannelChat: strPtr("-123")}, "channel_chat"},
		{SettingsPatch{ChannelChat: strPtr("-1001234567890"), ChannelEnabled: boolPtr(true)}, "channel_url"},
		{SettingsPatch{ChannelURL: strPtr("https://example.com/nora")}, "channel_url"},
		{SettingsPatch{ChannelURL: strPtr("http://t.me/nora")}, "channel_url"},
		{SettingsPatch{ChannelText: strPtr("a < b")}, "channel_text"},
		{SettingsPatch{HelpEnabled: boolPtr(true)}, "help_text"},
		{SettingsPatch{HelpText: strPtr(strings.Repeat("я", 3001))}, "help_text"},
		{SettingsPatch{HelpButtons: buttonsPtr(store.HelpButton{Label: "", URL: "https://a.example"})}, "help_buttons"},
		{SettingsPatch{HelpButtons: buttonsPtr(store.HelpButton{Label: "   ", URL: "https://a.example"})}, "help_buttons"},
		{SettingsPatch{HelpButtons: buttonsPtr(store.HelpButton{Label: strings.Repeat("я", 33), URL: "https://a.example"})}, "help_buttons"},
		{SettingsPatch{HelpButtons: buttonsPtr(store.HelpButton{Label: "a\nb", URL: "https://a.example"})}, "help_buttons"},
		{SettingsPatch{HelpButtons: buttonsPtr(store.HelpButton{Label: "a", URL: "javascript:alert(1)"})}, "help_buttons"},
		{SettingsPatch{HelpButtons: buttonsPtr(store.HelpButton{Label: "a", URL: "http://a.example"})}, "help_buttons"},
	} {
		_, err := svc.UpdateSettings(ctx, c.patch)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Fatalf("%+v: err = %v", c.patch, err)
		}
	}
	seven := make([]store.HelpButton, 7)
	for i := range seven {
		seven[i] = store.HelpButton{Label: "кнопка", URL: "https://a.example"}
	}
	if _, err := svc.UpdateSettings(ctx, SettingsPatch{HelpButtons: &seven}); err == nil || !strings.Contains(err.Error(), "at most 6") {
		t.Fatalf("seven buttons: %v", err)
	}
	st, err := svc.GetSettings(ctx)
	if err != nil || st.TermsEnabled || st.ChannelEnabled || st.HelpEnabled || st.ChannelChat != "" {
		t.Fatalf("rejected patches changed settings: %+v, %v", st, err)
	}
}

func TestBotSettingsFillChannelURLAndConfig(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	st, err := svc.UpdateSettings(ctx, SettingsPatch{ChannelChat: strPtr("@nora_vpn"), ChannelEnabled: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if st.ChannelURL != "https://t.me/nora_vpn" {
		t.Fatalf("channel url = %q", st.ChannelURL)
	}
	if st, err = svc.UpdateSettings(ctx, SettingsPatch{ChannelChat: strPtr("@other_vpn")}); err != nil || st.ChannelURL != "https://t.me/other_vpn" {
		t.Fatalf("auto url did not follow the channel: %q, %v", st.ChannelURL, err)
	}
	if st, err = svc.UpdateSettings(ctx, SettingsPatch{ChannelURL: strPtr("https://t.me/+Invite")}); err != nil {
		t.Fatal(err)
	}
	if st, err = svc.UpdateSettings(ctx, SettingsPatch{ChannelChat: strPtr("@third_vpn")}); err != nil || st.ChannelURL != "https://t.me/+Invite" {
		t.Fatalf("explicit url replaced: %q, %v", st.ChannelURL, err)
	}
	if _, err := svc.UpdateSettings(ctx, SettingsPatch{ChannelChat: strPtr("-1001234567890"), ChannelURL: strPtr("https://t.me/+AbCdEf")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateSettings(ctx, SettingsPatch{ChannelURL: strPtr("")}); err == nil {
		t.Fatal("cleared url of an enabled private channel")
	}
	help := []store.HelpButton{{Label: " Инструкции ", URL: "https://telegra.ph/how"}, {Label: "Поддержка", URL: "tg://resolve?domain=help"}}
	if _, err := svc.UpdateSettings(ctx, SettingsPatch{
		TermsEnabled: boolPtr(true), TermsURL: strPtr("https://telegra.ph/terms"), TermsText: strPtr("Прочитай <b>внимательно</b>"),
		HelpEnabled: boolPtr(true), HelpText: strPtr("<b>Помощь</b>"), HelpButtons: &help,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.BotConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := BotConfig{
		Terms:   BotTerms{Enabled: true, Text: "Прочитай <b>внимательно</b>", URL: "https://telegra.ph/terms", Version: 1},
		Channel: BotChannel{Enabled: true, Chat: "-1001234567890", URL: "https://t.me/+AbCdEf"},
		Help:    BotHelp{Enabled: true, Text: "<b>Помощь</b>", Buttons: []store.HelpButton{{Label: "Инструкции", URL: "https://telegra.ph/how"}, {Label: "Поддержка", URL: "tg://resolve?domain=help"}}},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("config = %+v\nwant %+v", cfg, want)
	}
	st, err = svc.BumpTerms(ctx)
	if err != nil || st.TermsVersion != 2 {
		t.Fatalf("bump: %d, %v", st.TermsVersion, err)
	}
	if cfg, _ = svc.BotConfig(ctx); cfg.Terms.Version != 2 {
		t.Fatalf("config version = %d", cfg.Terms.Version)
	}
}
