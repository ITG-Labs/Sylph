package store

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkUser(t *testing.T, s *Store) *model.User {
	t.Helper()
	u := &model.User{SubToken: model.NewToken()}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func mkPlan(t *testing.T, s *Store) *model.Plan {
	t.Helper()
	p := &model.Plan{Name: model.NewToken(), PriceRub: 100, PeriodDays: 30}
	if err := s.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRenewCreatesThenExtends(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	now := time.Now().UTC()
	id1, err := s.RenewSubscription(ctx, u.ID, p.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.GetActiveSubscription(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub.ID != id1 {
		t.Fatalf("sub id = %d, want %d", sub.ID, id1)
	}
	if !sub.ExpiresAt.After(now.Add(29 * 24 * time.Hour)) {
		t.Fatalf("expires = %v", sub.ExpiresAt)
	}
	id2, err := s.RenewSubscription(ctx, u.ID, p.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id1 {
		t.Fatalf("renew should extend same sub: %d vs %d", id2, id1)
	}
	sub, _ = s.GetActiveSubscription(ctx, u.ID)
	if !sub.ExpiresAt.After(now.Add(59 * 24 * time.Hour)) {
		t.Fatalf("after 2nd renew expires = %v", sub.ExpiresAt)
	}
}

func TestExpireOverdue(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	now := time.Now().UTC()
	if _, err := s.RenewSubscription(ctx, u.ID, p.ID, now); err != nil {
		t.Fatal(err)
	}
	n, err := s.ExpireOverdue(ctx, now.Add(31*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expired = %d, want 1", n)
	}
	_, err = s.GetActiveSubscription(ctx, u.ID)
	if err == nil {
		t.Fatal("no active sub expected")
	}
}
