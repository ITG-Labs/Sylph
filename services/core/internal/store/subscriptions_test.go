package store

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkUser(t *testing.T, s *Store) *model.User {
	t.Helper()
	u := &model.User{SubToken: model.NewToken(), Status: "active"}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func mkPlan(t *testing.T, s *Store) *model.Plan {
	t.Helper()
	p := &model.Plan{Name: model.NewToken(), PriceRub: 100, PeriodDays: 30, LocationGroupID: testGroup(t, s)}
	if err := s.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExpireOverdue(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	p := mkPlan(t, s)
	now := time.Now().UTC()
	if _, err := s.ApplyPayment(ctx, u.ID, p.ID, now); err != nil {
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
