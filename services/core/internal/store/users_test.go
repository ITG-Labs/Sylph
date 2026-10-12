package store

import (
	"context"
	"errors"
	"sync"
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
	token := model.NewToken()
	key, err := s.RotateUserCredentials(ctx, u.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	if key == uuid1 || key == "" {
		t.Fatalf("rotated key = %q", key)
	}
	keys, _ = s.GetUserKeys(ctx, u.ID)
	if len(keys) != 1 || keys[0].UUID != key {
		t.Fatalf("after rotate keys = %+v", keys)
	}
	if _, err := s.GetUserBySubToken(ctx, u.SubToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token still works: %v", err)
	}
	got, err := s.GetUserBySubToken(ctx, token)
	if err != nil || got.ID != u.ID {
		t.Fatalf("new token = %+v, %v", got, err)
	}
}

func TestRotateUnknownUserIsNotFound(t *testing.T) {
	s := setupStore(t)
	if _, err := s.RotateUserCredentials(context.Background(), 424242, model.NewToken()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestResetDevicesUnknownUserIsNotFound(t *testing.T) {
	s := setupStore(t)
	if err := s.ResetDevices(context.Background(), 424242); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRegisterTelegramUserIsIdempotent(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	first, created, err := s.RegisterTelegramUser(ctx, 7001, model.NewToken(), model.NewUUID())
	if err != nil || !created {
		t.Fatalf("first register = %+v, %v, %v", first, created, err)
	}
	again, created, err := s.RegisterTelegramUser(ctx, 7001, model.NewToken(), model.NewUUID())
	if err != nil || created || again.ID != first.ID || again.SubToken != first.SubToken {
		t.Fatalf("second register = %+v, %v, %v; want the first user back", again, created, err)
	}
	keys, err := s.GetUserKeys(ctx, first.ID)
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys = %+v, %v", keys, err)
	}
}

func TestRegisterTelegramUserConcurrentlyCreatesOne(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	ids := make([]int64, 8)
	errs := make([]error, 8)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, _, err := s.RegisterTelegramUser(ctx, 7002, model.NewToken(), model.NewUUID())
			errs[i] = err
			if u != nil {
				ids[i] = u.ID
			}
		}()
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Fatalf("register %d = %d, %v; first = %d", i, ids[i], errs[i], ids[0])
		}
	}
	var users, keys int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM user_keys)`).Scan(&users, &keys); err != nil {
		t.Fatal(err)
	}
	if users != 1 || keys != 1 {
		t.Fatalf("users = %d, keys = %d", users, keys)
	}
}
