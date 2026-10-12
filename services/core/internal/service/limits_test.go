package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

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
	if _, err := svc.store.ApplyPayment(ctx, u.ID, p.ID, time.Now().UTC()); err != nil {
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

func onNode(t *testing.T, svc *Service, n *model.Node, key string) bool {
	t.Helper()
	keys, err := svc.store.ListNodeUserKeys(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	return slices.Contains(keys, key)
}

func TestTrafficLimitTakesUserOffNodes(t *testing.T) {
	svc, logs := setupServiceWithLogs(t)
	ctx := context.Background()
	u, key, n := limitedUserOnNode(t, svc, 300)
	if !onNode(t, svc, n, key) {
		t.Fatal("paid user is not on the node")
	}
	if _, err := svc.RecordNodeDeltas(ctx, "node-1", "b-1", []model.TrafficDelta{{KeyUUID: key, BytesUp: 600, BytesDown: 600}}); err != nil {
		t.Fatal(err)
	}
	if onNode(t, svc, n, key) {
		t.Fatal("exhausted user is still on the node")
	}
	if !logs.has("traffic_period_exhausted") {
		t.Fatalf("no exhaustion log: %s", logs)
	}
	if got, _ := svc.store.GetUserBySubToken(ctx, u.SubToken); got.Status != "active" {
		t.Fatalf("user status = %q; exhaustion belongs to the period", got.Status)
	}
}

func TestDuplicateBatchCountedOnce(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, key, n := limitedUserOnNode(t, svc, 310)
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
	if !onNode(t, svc, n, key) {
		t.Fatal("duplicate pushed user over the limit")
	}
}

func TestBatchAndCounterAreOneStep(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	u, key, n := limitedUserOnNode(t, svc, 311)
	batch := []model.TrafficDelta{{KeyUUID: key, BytesUp: 700, BytesDown: 700}}
	if _, err := svc.store.RecordDeltaBatch(ctx, n.ID, "b-1", batch); err != nil {
		t.Fatal(err)
	}
	if used := trafficUsed(t, svc, u.ID); used != 1400 {
		t.Fatalf("traffic_used = %d, want 1400 right after the batch", used)
	}
	dup, err := svc.RecordNodeDeltas(ctx, "node-1", "b-1", batch)
	if err != nil || !dup {
		t.Fatalf("retry: dup = %v, err = %v; want duplicate", dup, err)
	}
	if used := trafficUsed(t, svc, u.ID); used != 1400 {
		t.Fatalf("retry changed traffic_used to %d", used)
	}
}

func TestForgedHugeDeltaDoesNotBlockHonestNode(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	_, key, n := limitedUserOnNode(t, svc, 312)
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
	if onNode(t, svc, n, key) {
		t.Fatal("inflated user is still on the node")
	}
}

func TestCleanupHistoryRejectsBadRetention(t *testing.T) {
	svc := setupService(t)
	for _, days := range []int{0, -5, 3651} {
		if _, err := svc.CleanupHistory(context.Background(), days, nil, time.Now()); err == nil {
			t.Fatalf("retention %d accepted", days)
		}
	}
}
