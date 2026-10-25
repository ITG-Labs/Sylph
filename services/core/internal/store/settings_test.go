package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSettingsDefaultsAndPartialUpdate(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	got, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.SubsTitle != "Sylph" || got.SubsSupportURL != "" || got.UpdatedAt.IsZero() {
		t.Fatalf("defaults = %+v", got)
	}
	upd, err := s.UpdateSettings(ctx, SettingsPatch{SubsSupportURL: strp("https://t.me/help")})
	if err != nil {
		t.Fatal(err)
	}
	if upd.SubsTitle != "Sylph" || upd.SubsSupportURL != "https://t.me/help" || upd.UpdatedAt.Before(got.UpdatedAt) {
		t.Fatalf("after support = %+v", upd)
	}
	upd, err = s.UpdateSettings(ctx, SettingsPatch{SubsTitle: strp("Нора VPN")})
	if err != nil {
		t.Fatal(err)
	}
	if upd.SubsTitle != "Нора VPN" || upd.SubsSupportURL != "https://t.me/help" {
		t.Fatalf("after title = %+v", upd)
	}
	again, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, upd) {
		t.Fatalf("stored %+v, returned %+v", again, upd)
	}
}

func TestBotSettingsAndTerms(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	on := true
	if _, err := s.UpdateSettings(ctx, SettingsPatch{TermsEnabled: &on}); !errors.Is(err, ErrConflict) {
		t.Fatalf("terms on without url: %v", err)
	}
	buttons := []HelpButton{{Label: "Инструкции", URL: "https://telegra.ph/how"}, {Label: "Поддержка", URL: "tg://resolve?domain=help"}}
	st, err := s.UpdateSettings(ctx, SettingsPatch{
		TermsEnabled: &on, TermsURL: strp("https://telegra.ph/terms"), TermsText: strp("Прочитай"),
		ChannelEnabled: &on, ChannelChat: strp("@nora"), ChannelURL: strp("https://t.me/nora"),
		HelpEnabled: &on, HelpText: strp("<b>Помощь</b>"), HelpButtons: &buttons,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !st.TermsEnabled || st.TermsVersion != 1 || st.ChannelChat != "@nora" || !reflect.DeepEqual(st.HelpButtons, buttons) {
		t.Fatalf("settings = %+v", st)
	}
	st, err = s.UpdateSettings(ctx, SettingsPatch{HelpText: strp("ещё")})
	if err != nil || !reflect.DeepEqual(st.HelpButtons, buttons) {
		t.Fatalf("buttons lost on an unrelated patch: %+v, %v", st.HelpButtons, err)
	}
	none := []HelpButton{}
	if st, err = s.UpdateSettings(ctx, SettingsPatch{HelpButtons: &none}); err != nil || len(st.HelpButtons) != 0 || st.HelpButtons == nil {
		t.Fatalf("buttons cleared = %#v, %v", st.HelpButtons, err)
	}

	u := tgUser(t, s, 501)
	if got, err := s.LatestTerms(ctx, u.ID); err != nil || got != nil {
		t.Fatalf("fresh user terms = %+v, %v", got, err)
	}
	if err := s.AcceptTerms(ctx, u.ID, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("future version: %v", err)
	}
	if err := s.AcceptTerms(ctx, 999999, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	for range 2 {
		if err := s.AcceptTerms(ctx, u.ID, 1); err != nil {
			t.Fatal(err)
		}
	}
	st, err = s.BumpTermsVersion(ctx)
	if err != nil || st.TermsVersion != 2 {
		t.Fatalf("bump = %d, %v", st.TermsVersion, err)
	}
	if err := s.AcceptTerms(ctx, u.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version: %v", err)
	}
	if err := s.AcceptTerms(ctx, u.ID, 2); err != nil {
		t.Fatal(err)
	}
	got, err := s.LatestTerms(ctx, u.ID)
	if err != nil || got == nil || got.Version != 2 || got.AcceptedAt.IsZero() {
		t.Fatalf("latest = %+v, %v", got, err)
	}
}
