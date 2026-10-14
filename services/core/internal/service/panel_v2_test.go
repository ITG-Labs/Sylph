package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func strPtr(s string) *string { return &s }

func TestParseUserSearch(t *testing.T) {
	token := strings.Repeat("ab", 16)
	n := int64(8001)
	for _, c := range []struct {
		in   string
		want userSearch
	}{
		{"", userSearch{}},
		{"  8001 ", userSearch{Number: &n}},
		{"https://subs.example.com/" + token + "?x=1", userSearch{Token: token}},
		{"subs.example.com/" + token + "/", userSearch{Token: token}},
		{token, userSearch{Token: token}},
		{strings.ToUpper(token), userSearch{Token: token}},
		{"@IvanTop", userSearch{Username: "IvanTop"}},
		{"  @ivan_1 ", userSearch{Username: "ivan_1"}},
		{"Иван", userSearch{Text: "Иван"}},
		{"ivan petrov", userSearch{Text: "ivan petrov"}},
		{"ab12", userSearch{Text: "ab12"}},
		{"100%", userSearch{Text: "100%"}},
	} {
		got, err := parseUserSearch(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if (got.Number == nil) != (c.want.Number == nil) || (got.Number != nil && *got.Number != *c.want.Number) ||
			got.Token != c.want.Token || got.Username != c.want.Username || got.Text != c.want.Text {
			t.Fatalf("%q: got %+v, want %+v", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"99999999999999999999", "@", "@bad nick", "@ник", "@" + strings.Repeat("a", 33), strings.Repeat("я", 65), "a\x00b", "/"} {
		_, err := parseUserSearch(bad)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "q" {
			t.Fatalf("%q: err = %v", bad, err)
		}
	}
}

func TestCleanTelegramProfile(t *testing.T) {
	long := strings.Repeat("я", 70)
	got := cleanTelegramProfile(TelegramProfileInput{Username: "@Ivan_P", FirstName: " Ив\x00ан\u2028 ", LastName: long})
	if got.Username == nil || *got.Username != "Ivan_P" || got.FirstName != "Иван" || utf8.RuneCountInString(got.LastName) != 64 {
		t.Fatalf("cleaned = %+v (%q)", got, got.FirstName)
	}
	for _, nick := range []string{"", "  ", "bad nick", "ник", strings.Repeat("a", 33), "@"} {
		if got := cleanTelegramProfile(TelegramProfileInput{Username: nick, FirstName: "x"}); got.Username != nil {
			t.Fatalf("nick %q kept as %q", nick, *got.Username)
		}
	}
	if got := cleanTelegramProfile(TelegramProfileInput{FirstName: "\xff\xfe" + strings.Repeat(" ", 3)}); got.FirstName != "��" {
		t.Fatalf("broken utf-8 = %q", got.FirstName)
	}
}

func TestSaveTelegramProfileCleans(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, _, err := svc.RegisterUser(ctx, 7001)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveTelegramProfile(ctx, u.ID, TelegramProfileInput{Username: "bad nick", FirstName: "Иван\n"}); err != nil {
		t.Fatal(err)
	}
	card, err := svc.UserCard(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.Telegram == nil || card.Telegram.Username != nil || card.Telegram.Name != "Иван" {
		t.Fatalf("telegram = %+v", card.Telegram)
	}
}

func TestSettingsValidation(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	for _, c := range []struct {
		patch SettingsPatch
		field string
	}{
		{SettingsPatch{SubsTitle: strPtr("")}, "subs_title"},
		{SettingsPatch{SubsTitle: strPtr("   ")}, "subs_title"},
		{SettingsPatch{SubsTitle: strPtr(strings.Repeat("я", 65))}, "subs_title"},
		{SettingsPatch{SubsTitle: strPtr("a\nb")}, "subs_title"},
		{SettingsPatch{SubsSupportURL: strPtr("t.me/help")}, "subs_support_url"},
		{SettingsPatch{SubsSupportURL: strPtr("javascript:alert(1)")}, "subs_support_url"},
		{SettingsPatch{SubsSupportURL: strPtr("ftp://x.example")}, "subs_support_url"},
		{SettingsPatch{SubsSupportURL: strPtr("https://")}, "subs_support_url"},
		{SettingsPatch{SubsSupportURL: strPtr("https://x.example/a b")}, "subs_support_url"},
		{SettingsPatch{SubsSupportURL: strPtr("https://x.example/\r\nX-Evil: 1")}, "subs_support_url"},
		{SettingsPatch{SubsSupportURL: strPtr("https://x.example/" + strings.Repeat("a", 512))}, "subs_support_url"},
	} {
		_, err := svc.UpdateSettings(ctx, c.patch)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Fatalf("%+v: err = %v", c.patch, err)
		}
	}
	got, err := svc.UpdateSettings(ctx, SettingsPatch{SubsTitle: strPtr("Нора"), SubsSupportURL: strPtr("https://t.me/nora_help")})
	if err != nil {
		t.Fatal(err)
	}
	if got.SubsTitle != "Нора" || got.SubsSupportURL != "https://t.me/nora_help" {
		t.Fatalf("settings = %+v", got)
	}
	got, err = svc.UpdateSettings(ctx, SettingsPatch{SubsSupportURL: strPtr("")})
	if err != nil {
		t.Fatal(err)
	}
	if got.SubsTitle != "Нора" || got.SubsSupportURL != "" {
		t.Fatalf("after clearing support = %+v", got)
	}
}

func TestGroupRenameAndDeleteErrors(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	a, err := svc.CreateGroup(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateGroup(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	var fe *FieldError
	if _, err := svc.RenameGroup(ctx, a.ID, " "); !errors.As(err, &fe) || fe.Field != "name" {
		t.Fatalf("blank name: %v", err)
	}
	var ce *ConflictError
	if _, err := svc.RenameGroup(ctx, a.ID, "b"); !errors.As(err, &ce) || ce.Field != "name" {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := svc.RenameGroup(ctx, 999, "c"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := svc.CreatePlanChecked(ctx, &model.Plan{Name: "p", PriceRub: 1, PeriodDays: 30, LocationGroupID: &a.ID, Visibility: model.VisibilityPublic}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteGroup(ctx, a.ID); !errors.As(err, &ce) || ce.Field != "group" {
		t.Fatalf("non-empty: %v", err)
	}
	if err := svc.DeleteGroup(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestSubscriptionCarriesBrand(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	v := f.view(t, "")
	if v.Title != "Sylph" || v.SupportURL != "" {
		t.Fatalf("default brand = %q %q", v.Title, v.SupportURL)
	}
	if _, err := f.svc.UpdateSettings(context.Background(), SettingsPatch{SubsTitle: strPtr("Нора"), SubsSupportURL: strPtr("https://t.me/nora")}); err != nil {
		t.Fatal(err)
	}
	if v := f.view(t, ""); v.Title != "Нора" || v.SupportURL != "https://t.me/nora" {
		t.Fatalf("brand = %q %q", v.Title, v.SupportURL)
	}
	u, _, err := f.svc.RegisterUser(context.Background(), 901)
	if err != nil {
		t.Fatal(err)
	}
	v, err = f.svc.Subscription(context.Background(), SubscriptionRequest{SubToken: u.SubToken})
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusExpired || v.Title != "Нора" || v.SupportURL != "https://t.me/nora" {
		t.Fatalf("expired view = %+v", v)
	}
}
