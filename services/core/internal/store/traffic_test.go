package store

import (
	"context"
	"errors"
	"math"
	"sync"
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
	dup, err := s.RecordDeltaBatch(ctx, n.ID, "b-1", []model.TrafficDelta{
		{KeyUUID: k, BytesUp: 100, BytesDown: 200},
		{KeyUUID: k, BytesUp: 300, BytesDown: 400},
	})
	if err != nil || dup {
		t.Fatalf("dup = %v, err = %v", dup, err)
	}
	sum, err := s.SumTrafficSince(ctx, u.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sum != 1000 {
		t.Fatalf("sum = %d, want 1000", sum)
	}
}

func TestRegisterDevice(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	limit := 2
	for _, d := range []DeviceInfo{{HWID: "hw1", UserAgent: "Happ/1.0"}, {HWID: "hw1", UserAgent: "Happ/1.1", OS: "iOS", Model: "iPhone"}, {HWID: "hw2"}} {
		ok, err := s.RegisterDevice(ctx, u.ID, d, &limit)
		if err != nil || !ok {
			t.Fatalf("%+v: ok=%v err=%v", d, ok, err)
		}
	}
	if ok, err := s.RegisterDevice(ctx, u.ID, DeviceInfo{HWID: "hw3"}, &limit); err != nil || ok {
		t.Fatalf("over limit: ok=%v err=%v", ok, err)
	}
	if ok, err := s.RegisterDevice(ctx, u.ID, DeviceInfo{HWID: "hw3"}, nil); err != nil || !ok {
		t.Fatalf("no limit: ok=%v err=%v", ok, err)
	}
	var ua, osName, model string
	if err := s.pool.QueryRow(ctx, `SELECT user_agent, device_os, device_model FROM devices WHERE user_id = $1 AND fingerprint = 'hw1'`, u.ID).Scan(&ua, &osName, &model); err != nil {
		t.Fatal(err)
	}
	if ua != "Happ/1.1" || osName != "iOS" || model != "iPhone" {
		t.Fatalf("device row = %q %q %q", ua, osName, model)
	}
	count, err := s.CountDevices(ctx, u.ID)
	if err != nil || count != 3 {
		t.Fatalf("count = %d, err = %v", count, err)
	}
	if err := s.ResetDevices(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if count, _ = s.CountDevices(ctx, u.ID); count != 0 {
		t.Fatalf("after reset count = %d", count)
	}
	if _, err := s.RegisterDevice(ctx, 424242, DeviceInfo{HWID: "x"}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v, want ErrNotFound", err)
	}
}

func TestRecordDeltaBatchDeduplicates(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := mkUser(t, s)
	k := model.NewUUID()
	if err := s.AddUserKey(ctx, u.ID, k); err != nil {
		t.Fatal(err)
	}
	n1 := &model.Node{CertCN: "node-1", Name: "n1"}
	n2 := &model.Node{CertCN: "node-2", Name: "n2"}
	if err := s.UpsertNode(ctx, n1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(ctx, n2); err != nil {
		t.Fatal(err)
	}
	batch := []model.TrafficDelta{{KeyUUID: k, BytesUp: 100, BytesDown: 200}}
	if dup, err := s.RecordDeltaBatch(ctx, n1.ID, "b-1", batch); err != nil || dup {
		t.Fatalf("first: dup = %v, err = %v", dup, err)
	}
	if dup, err := s.RecordDeltaBatch(ctx, n1.ID, "b-1", batch); err != nil || !dup {
		t.Fatalf("repeat: dup = %v, err = %v; want duplicate", dup, err)
	}
	if dup, err := s.RecordDeltaBatch(ctx, n2.ID, "b-1", batch); err != nil || dup {
		t.Fatalf("same id from another node: dup = %v, err = %v; want new batch", dup, err)
	}
	sum, err := s.SumTrafficSince(ctx, u.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sum != 600 {
		t.Fatalf("sum = %d, want 600 (node-1 once + node-2 once)", sum)
	}
}

func TestRecordDeltaBatchConcurrentRepeats(t *testing.T) {
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
	batch := []model.TrafficDelta{{KeyUUID: k, BytesUp: 10, BytesDown: 0}}
	var wg sync.WaitGroup
	results := make([]bool, 8)
	errs := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = s.RecordDeltaBatch(ctx, n.ID, "b-race", batch)
		}()
	}
	wg.Wait()
	fresh := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if !results[i] {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("fresh writes = %d, want exactly 1", fresh)
	}
	sum, err := s.SumTrafficSince(ctx, u.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sum != 10 {
		t.Fatalf("sum = %d, want 10", sum)
	}
}

func TestSumTrafficSaturatesInsteadOfFailing(t *testing.T) {
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
	huge := int64(1) << 62
	forged := []model.TrafficDelta{
		{KeyUUID: k, BytesUp: huge, BytesDown: huge},
		{KeyUUID: k, BytesUp: huge, BytesDown: 0},
	}
	if _, err := s.RecordDeltaBatch(ctx, n.ID, "b-forged", forged); err != nil {
		t.Fatal(err)
	}
	sum, err := s.SumTrafficSince(ctx, u.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("sum over int64 must saturate, not fail: %v", err)
	}
	if sum != math.MaxInt64 {
		t.Fatalf("sum = %d, want MaxInt64", sum)
	}
}
