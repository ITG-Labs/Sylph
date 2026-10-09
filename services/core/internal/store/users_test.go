package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func setupStore(t *testing.T) *Store {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return NewStore(pool)
}

func TestCreateAndGetUser(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	tg := int64(100500)
	u := &model.User{TelegramID: &tg, SubToken: model.NewToken(), Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 {
		t.Fatal("id not filled")
	}
	got, err := s.GetUserByTelegramID(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID || got.SubToken != u.SubToken {
		t.Fatalf("got %+v, want %+v", got, u)
	}
	_, err = s.GetUserBySubToken(ctx, u.SubToken)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetUserNotFound(t *testing.T) {
	s := setupStore(t)
	_, err := s.GetUserByTelegramID(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUserKeysAndRotate(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := &model.User{SubToken: model.NewToken()}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	uuid1 := model.NewUUID()
	if err := s.AddUserKey(ctx, u.ID, uuid1); err != nil {
		t.Fatal(err)
	}
	keys, err := s.GetUserKeys(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].UUID != uuid1 {
		t.Fatalf("keys = %+v", keys)
	}
	newKeys, err := s.RotateUserKeys(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(newKeys) != 1 || newKeys[0].UUID == uuid1 {
		t.Fatalf("rotated keys = %+v", newKeys)
	}
	keys, _ = s.GetUserKeys(ctx, u.ID)
	if len(keys) != 1 {
		t.Fatalf("after rotate keys = %+v", keys)
	}
}
