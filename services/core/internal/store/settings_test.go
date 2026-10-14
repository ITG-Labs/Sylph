package store

import (
	"context"
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
	if again != upd {
		t.Fatalf("stored %+v, returned %+v", again, upd)
	}
}
