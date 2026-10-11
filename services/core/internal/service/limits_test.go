package service

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestTrafficLimitDisablesUser(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, key, _ := svc.RegisterUser(ctx, 300)
	limit := int64(1000)
	p := &model.Plan{Name: "limited", PriceRub: 100, PeriodDays: 30, TrafficLimitBytes: &limit}
	if err := svc.store.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RenewSubscription(ctx, u.ID, p.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := svc.store.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	_, err := svc.RecordNodeDeltas(ctx, "node-1", "b-1", []model.TrafficDelta{
		{KeyUUID: key, BytesUp: 600, BytesDown: 600},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.store.GetUserBySubToken(ctx, u.SubToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "expired_traffic" {
		t.Fatalf("status = %q, want expired_traffic", got.Status)
	}
}

func TestRegisterDeviceLimit(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, _, _ := svc.RegisterUser(ctx, 400)
	devices := 2
	p := &model.Plan{Name: "dev", PriceRub: 100, PeriodDays: 30, DeviceLimit: &devices}
	if err := svc.store.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RenewSubscription(ctx, u.ID, p.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	ok, err := svc.RegisterDevice(ctx, u.SubToken, "fp1", "Happ/1")
	if err != nil || !ok {
		t.Fatalf("first device: ok=%v err=%v", ok, err)
	}
	ok, err = svc.RegisterDevice(ctx, u.SubToken, "fp2", "Happ/1")
	if err != nil || !ok {
		t.Fatalf("second device: ok=%v err=%v", ok, err)
	}
	ok, err = svc.RegisterDevice(ctx, u.SubToken, "fp3", "v2rayNG")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("third device should be rejected")
	}
}

func limitedUserOnNode(t *testing.T, svc *Service, tg int64) (*model.User, string, *model.Node) {
	t.Helper()
	ctx := context.Background()
	u, key, err := svc.RegisterUser(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	limit := int64(1000)
	p := &model.Plan{Name: model.NewToken(), PriceRub: 100, PeriodDays: 30, TrafficLimitBytes: &limit}
	if err := svc.store.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.RenewSubscription(ctx, u.ID, p.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := svc.store.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	return u, key, n
}

func trafficUsed(t *testing.T, svc *Service, userID int64) int64 {
	t.Helper()
	sub, err := svc.store.GetActiveSubscription(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return sub.TrafficUsedBytes
}

func TestDuplicateBatchCountedOnce(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, key, _ := limitedUserOnNode(t, svc, 310)
	batch := []model.TrafficDelta{{KeyUUID: key, BytesUp: 300, BytesDown: 300}}
	dup, err := svc.RecordNodeDeltas(ctx, "node-1", "b-1", batch)
	if err != nil || dup {
		t.Fatalf("first: dup = %v, err = %v", dup, err)
	}
	dup, err = svc.RecordNodeDeltas(ctx, "node-1", "b-1", batch)
	if err != nil || !dup {
		t.Fatalf("repeat: dup = %v, err = %v; want duplicate", dup, err)
	}
	if used := trafficUsed(t, svc, u.ID); used != 600 {
		t.Fatalf("traffic_used = %d, want 600", used)
	}
	got, err := svc.store.GetUserBySubToken(ctx, u.SubToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" {
		t.Fatalf("status = %q: duplicate pushed user over the limit", got.Status)
	}
}

func TestDuplicateBatchFinishesRecompute(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, key, n := limitedUserOnNode(t, svc, 311)
	batch := []model.TrafficDelta{{KeyUUID: key, BytesUp: 700, BytesDown: 700}}
	if _, err := svc.store.RecordDeltaBatch(ctx, n.ID, "b-crashed", batch); err != nil {
		t.Fatal(err)
	}
	if used := trafficUsed(t, svc, u.ID); used != 0 {
		t.Fatalf("precondition: traffic_used = %d, want 0 before recompute", used)
	}
	dup, err := svc.RecordNodeDeltas(ctx, "node-1", "b-crashed", batch)
	if err != nil || !dup {
		t.Fatalf("retry: dup = %v, err = %v; want duplicate", dup, err)
	}
	if used := trafficUsed(t, svc, u.ID); used != 1400 {
		t.Fatalf("traffic_used = %d, want 1400 after retry", used)
	}
	got, err := svc.store.GetUserBySubToken(ctx, u.SubToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "expired_traffic" {
		t.Fatalf("status = %q, want expired_traffic after retry", got.Status)
	}
}

func TestForgedHugeDeltaDoesNotBlockHonestNode(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, key, n := limitedUserOnNode(t, svc, 312)
	huge := int64(1) << 62
	if _, err := svc.store.RecordDeltaBatch(ctx, n.ID, "b-forged", []model.TrafficDelta{
		{KeyUUID: key, BytesUp: huge, BytesDown: huge},
		{KeyUUID: key, BytesUp: huge, BytesDown: huge},
	}); err != nil {
		t.Fatal(err)
	}
	dup, err := svc.RecordNodeDeltas(ctx, "node-1", "b-honest", []model.TrafficDelta{{KeyUUID: key, BytesUp: 10, BytesDown: 10}})
	if err != nil || dup {
		t.Fatalf("honest batch after forged one: dup = %v, err = %v; accounting must not get stuck", dup, err)
	}
	got, err := svc.store.GetUserBySubToken(ctx, u.SubToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "expired_traffic" {
		t.Fatalf("status = %q, want expired_traffic for the inflated user", got.Status)
	}
}
