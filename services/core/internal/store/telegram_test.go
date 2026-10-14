package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func strp(s string) *string { return &s }

func profileState(t *testing.T, s *Store, userID int64) (string, time.Time) {
	t.Helper()
	var xmin string
	var at time.Time
	if err := s.pool.QueryRow(context.Background(),
		`SELECT xmin::text, updated_at FROM telegram_profiles WHERE user_id = $1`, userID).Scan(&xmin, &at); err != nil {
		t.Fatal(err)
	}
	return xmin, at
}

func TestTelegramProfileWritesOnlyChanges(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := tgUser(t, s, 2001)
	n := deltaNode(t, s)
	before := nodeVersion(t, s, n.CertCN)
	if err := s.SaveTelegramProfile(ctx, u.ID, TelegramProfile{Username: strp("Ivan_P"), FirstName: "Иван", LastName: "Петров"}); err != nil {
		t.Fatal(err)
	}
	card, err := s.UserCard(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tg := card.Telegram; tg == nil || tg.Username == nil || *tg.Username != "Ivan_P" || tg.Name != "Иван Петров" || tg.UpdatedAt.IsZero() {
		t.Fatalf("card telegram = %+v", card.Telegram)
	}
	xmin, at := profileState(t, s, u.ID)
	if err := s.SaveTelegramProfile(ctx, u.ID, TelegramProfile{Username: strp("Ivan_P"), FirstName: "Иван", LastName: "Петров"}); err != nil {
		t.Fatal(err)
	}
	if x, a := profileState(t, s, u.ID); x != xmin || !a.Equal(at) {
		t.Fatalf("unchanged profile was rewritten: xmin %s -> %s", xmin, x)
	}
	if err := s.SaveTelegramProfile(ctx, u.ID, TelegramProfile{FirstName: "Иван"}); err != nil {
		t.Fatal(err)
	}
	card, err = s.UserCard(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tg := card.Telegram; tg.Username != nil || tg.Name != "Иван" {
		t.Fatalf("after dropping the nick: %+v", tg)
	}
	if x, _ := profileState(t, s, u.ID); x == xmin {
		t.Fatal("changed profile was not written")
	}
	if got := nodeVersion(t, s, n.CertCN); got != before {
		t.Fatalf("profile bumped node version %d -> %d", before, got)
	}
}

func TestTelegramProfileUnknownUser(t *testing.T) {
	s := setupStore(t)
	err := s.SaveTelegramProfile(context.Background(), 999, TelegramProfile{FirstName: "x"})
	if !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("err = %v", err)
	}
}

func TestUserCardWithoutProfile(t *testing.T) {
	s := setupStore(t)
	u := tgUser(t, s, 2002)
	card, err := s.UserCard(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.Telegram != nil {
		t.Fatalf("telegram = %+v", card.Telegram)
	}
	rows := listUsers(t, s, UserFilter{Limit: 10})
	if len(rows) != 1 || rows[0].TelegramUsername != nil || rows[0].TelegramName != nil {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestListUsersByNickAndName(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	ivan := tgUser(t, s, 3001)
	maria := tgUser(t, s, 3002)
	anon := tgUser(t, s, 3003)
	tgUser(t, s, 3004)
	for _, p := range []struct {
		id int64
		p  TelegramProfile
	}{
		{ivan.ID, TelegramProfile{Username: strp("IvanTop"), FirstName: "Иван", LastName: "Петров"}},
		{maria.ID, TelegramProfile{Username: strp("mary_100%"), FirstName: "Мария"}},
		{anon.ID, TelegramProfile{FirstName: "Ivanka"}},
	} {
		if err := s.SaveTelegramProfile(ctx, p.id, p.p); err != nil {
			t.Fatal(err)
		}
	}
	rows := listUsers(t, s, UserFilter{Limit: 10, Number: &ivan.ID})
	if len(rows) != 1 || rows[0].TelegramUsername == nil || *rows[0].TelegramUsername != "IvanTop" || rows[0].TelegramName == nil || *rows[0].TelegramName != "Иван Петров" {
		t.Fatalf("row = %+v", rows)
	}
	for _, c := range []struct {
		name string
		f    UserFilter
		want []int64
	}{
		{"nick prefix any case", UserFilter{Username: "ivant"}, []int64{ivan.ID}},
		{"nick is a prefix, not a substring", UserFilter{Username: "top"}, []int64{}},
		{"nick prefix does not match a name", UserFilter{Username: "ivank"}, []int64{}},
		{"text in nick and names", UserFilter{Text: "IVAN"}, []int64{anon.ID, ivan.ID}},
		{"text in a cyrillic name any case", UserFilter{Text: "петр"}, []int64{ivan.ID}},
		{"text with like wildcards is literal", UserFilter{Text: "y_100%"}, []int64{maria.ID}},
		{"underscore is not a wildcard", UserFilter{Text: "ry_1"}, []int64{maria.ID}},
		{"percent alone matches only a literal percent", UserFilter{Text: "%"}, []int64{maria.ID}},
		{"nothing", UserFilter{Text: "zzz"}, []int64{}},
	} {
		c.f.Limit = 10
		got := userIDs(listUsers(t, s, c.f))
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}
