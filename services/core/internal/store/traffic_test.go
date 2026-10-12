package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

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
	pay(t, s, u.ID, mkPlan(t, s).ID, time.Now().UTC())
	n1 := &model.Node{CertCN: "node-1", Name: "n1"}
	n2 := &model.Node{CertCN: "node-2", Name: "n2"}
	if err := s.UpsertNode(ctx, n1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(ctx, n2); err != nil {
		t.Fatal(err)
	}
	batch := []model.TrafficDelta{{KeyUUID: k, BytesUp: 100, BytesDown: 200}}
	if res, err := s.RecordDeltaBatch(ctx, n1.ID, "b-1", batch); err != nil || res.Duplicate {
		t.Fatalf("first: res = %+v, err = %v", res, err)
	}
	if res, err := s.RecordDeltaBatch(ctx, n1.ID, "b-1", batch); err != nil || !res.Duplicate {
		t.Fatalf("repeat: res = %+v, err = %v; want duplicate", res, err)
	}
	if res, err := s.RecordDeltaBatch(ctx, n2.ID, "b-1", batch); err != nil || res.Duplicate {
		t.Fatalf("same id from another node: res = %+v, err = %v; want new batch", res, err)
	}
	if sum := userPeriods(t, s, u.ID)[0].TrafficUsedBytes; sum != 600 {
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
	pay(t, s, u.ID, mkPlan(t, s).ID, time.Now().UTC())
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
			var res BatchResult
			res, errs[i] = s.RecordDeltaBatch(ctx, n.ID, "b-race", batch)
			results[i] = res.Duplicate
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
	if sum := userPeriods(t, s, u.ID)[0].TrafficUsedBytes; sum != 10 {
		t.Fatalf("sum = %d, want 10", sum)
	}
}
