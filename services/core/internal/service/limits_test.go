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
	err := svc.RecordNodeDeltas(ctx, "node-1", []model.TrafficDelta{
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
