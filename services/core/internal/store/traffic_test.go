package store

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestRecordAndSumTraffic(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	k := model.NewUUID()
	if err := s.AddUserKey(ctx, u.ID, k); err != nil {
		t.Fatal(err)
	}
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	err := s.RecordDeltas(ctx, n.ID, []model.TrafficDelta{
		{KeyUUID: k, BytesUp: 100, BytesDown: 200},
		{KeyUUID: k, BytesUp: 300, BytesDown: 400},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := s.SumTrafficSince(ctx, u.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sum != 1000 {
		t.Fatalf("sum = %d, want 1000", sum)
	}
}

func TestDevicesUpsert(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	if err := s.AddDevice(ctx, u.ID, "fp1", "Happ/1.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDevice(ctx, u.ID, "fp1", "Happ/1.1"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDevice(ctx, u.ID, "fp2", "v2rayNG/1.8"); err != nil {
		t.Fatal(err)
	}
	count, err := s.CountDevices(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if err := s.ResetDevices(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	count, _ = s.CountDevices(ctx, u.ID)
	if count != 0 {
		t.Fatalf("after reset count = %d", count)
	}
}
