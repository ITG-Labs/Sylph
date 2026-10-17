package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestServiceHeartbeatUpsertListForget(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	exp := time.Now().Add(400 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := s.UpsertServiceHeartbeat(ctx, model.ServiceHeartbeat{CertCN: "bot", Role: "bot", Version: "1.0.0", UptimeSec: 10, CertNotAfter: &exp}); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListServices(ctx)
	if err != nil || len(first) != 1 {
		t.Fatalf("services = %+v, %v", first, err)
	}
	b := first[0]
	if b.CertCN != "bot" || b.Role != "bot" || b.Version != "1.0.0" || b.UptimeSec != 10 || b.CertNotAfter == nil || !b.CertNotAfter.Equal(exp) || b.TelegramConflictAt != nil {
		t.Fatalf("first = %+v", b)
	}
	conflict := time.Now().UTC().Truncate(time.Second)
	if err := s.UpsertServiceHeartbeat(ctx, model.ServiceHeartbeat{CertCN: "bot", Role: "bot", Version: "1.1.0", UptimeSec: 70, CertNotAfter: &exp, TelegramConflictAt: &conflict}); err != nil {
		t.Fatal(err)
	}
	again, _ := s.ListServices(ctx)
	a := again[0]
	if a.Version != "1.1.0" || a.UptimeSec != 70 || a.TelegramConflictAt == nil || !a.TelegramConflictAt.Equal(conflict) || !a.CreatedAt.Equal(b.CreatedAt) || a.LastSeen.Before(b.LastSeen) {
		t.Fatalf("again = %+v (was %+v)", a, b)
	}
	if err := s.DeleteService(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteService(ctx, "bot"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
	if left, _ := s.ListServices(ctx); len(left) != 0 {
		t.Fatalf("left = %+v", left)
	}
}

func TestHealthMarksTrackFailureStreak(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	if marks, err := s.HealthMarks(ctx); err != nil || len(marks) != 0 {
		t.Fatalf("marks = %+v, %v", marks, err)
	}
	if err := s.MarkHealthFailed(ctx, model.HealthYooKassa, "timeout"); err != nil {
		t.Fatal(err)
	}
	m1, _ := s.HealthMarks(ctx)
	y1 := m1[model.HealthYooKassa]
	if y1.FailingSince == nil || y1.OKAt != nil || y1.Error != "timeout" {
		t.Fatalf("after first failure = %+v", y1)
	}
	if err := s.MarkHealthFailed(ctx, model.HealthYooKassa, "502"); err != nil {
		t.Fatal(err)
	}
	m2, _ := s.HealthMarks(ctx)
	y2 := m2[model.HealthYooKassa]
	if !y2.FailingSince.Equal(*y1.FailingSince) || y2.Error != "502" || !y2.FailedAt.After(*y1.FailedAt) && !y2.FailedAt.Equal(*y1.FailedAt) {
		t.Fatalf("streak start moved: %+v -> %+v", y1, y2)
	}
	if err := s.MarkHealthOK(ctx, model.HealthYooKassa); err != nil {
		t.Fatal(err)
	}
	m3, _ := s.HealthMarks(ctx)
	y3 := m3[model.HealthYooKassa]
	if y3.OKAt == nil || y3.FailingSince != nil {
		t.Fatalf("after ok = %+v", y3)
	}
	if err := s.MarkHealthOK(ctx, model.HealthCronExpire); err != nil {
		t.Fatal(err)
	}
	if m4, _ := s.HealthMarks(ctx); m4[model.HealthCronExpire].OKAt == nil {
		t.Fatalf("cron mark = %+v", m4)
	}
}

func TestNodeHeartbeatStoresCertExpiry(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "n1", Name: "n1", Address: "n1.example.com", Enabled: true}
	if err := s.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := s.TouchNodeHeartbeat(ctx, n.ID, model.NodeHeartbeat{AgentVersion: "a", CertNotAfter: &exp}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNode(ctx, n.ID)
	if err != nil || got.CertNotAfter == nil || !got.CertNotAfter.Equal(exp) {
		t.Fatalf("node = %+v, %v", got, err)
	}
}

func TestAttentionCountsCutoff(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	tg := int64(1)
	u := &model.User{TelegramID: &tg, SubToken: "t1"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := s.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status, source, created_at) VALUES ('old', $1, $2, 100, 'pending', 'yookassa', now() - interval '2 hours')`,
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status, source, created_at) VALUES ('new', $1, $2, 100, 'pending', 'yookassa', now() - interval '10 minutes')`,
	} {
		if _, err := s.pool.Exec(ctx, q, u.ID, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.AttentionCounts(ctx, time.Now().Add(-time.Hour))
	if err != nil || a.StuckPending != 1 {
		t.Fatalf("attention = %+v, %v", a, err)
	}
}
