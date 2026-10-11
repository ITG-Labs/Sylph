package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestLocationGroups(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g, err := s.CreateLocationGroup(ctx, "premium")
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "premium" || g.ID == 0 {
		t.Fatalf("group = %+v", g)
	}
	list, err := s.ListLocationGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
}

func TestPlans(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	limit := int64(100 * 1024 * 1024 * 1024)
	devices := 3
	p := &model.Plan{Name: "month", PriceRub: 200, PeriodDays: 30, TrafficLimitBytes: &limit, DeviceLimit: &devices}
	if err := s.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "month" || *got.TrafficLimitBytes != limit || *got.DeviceLimit != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestNodesUpsertAndHeartbeat(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "Frankfurt-1", Config: "{}"}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	n2 := &model.Node{CertCN: "node-1", Name: "Frankfurt-1-re", Config: `{"x":1}`}
	if err := s.UpsertNode(ctx, n2); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNodeByCN(ctx, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Frankfurt-1-re" || got.ID != n.ID {
		t.Fatalf("got %+v", got)
	}
	var cfgObj map[string]any
	if err := json.Unmarshal([]byte(got.Config), &cfgObj); err != nil {
		t.Fatalf("config not json: %v", err)
	}
	if v, ok := cfgObj["x"].(float64); !ok || v != 1 {
		t.Fatalf("config = %s", got.Config)
	}
	xrayUp, users := int64(7200), 412
	hb := model.NodeHeartbeat{AgentVersion: "0.1.0", XrayVersion: "25.1.1", AgentUptimeSec: 60, XrayUptimeSec: &xrayUp, ActiveUsers: &users}
	if err := s.TouchNodeHeartbeat(ctx, got.ID, hb); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetNodeByCN(ctx, "node-1")
	if got.LastHeartbeat == nil || got.AgentVersion == nil || *got.AgentVersion != "0.1.0" ||
		got.XrayVersion == nil || *got.XrayVersion != "25.1.1" ||
		got.AgentUptimeSec == nil || *got.AgentUptimeSec != 60 ||
		got.XrayUptimeSec == nil || *got.XrayUptimeSec != 7200 ||
		got.ActiveUsers == nil || *got.ActiveUsers != 412 {
		t.Fatalf("heartbeat not saved: %+v", got)
	}
	_, err = s.GetNodeByCN(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestHeartbeatUnknownsBecomeNull(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	xrayUp, users := int64(10), 3
	if err := s.TouchNodeHeartbeat(ctx, n.ID, model.NodeHeartbeat{AgentVersion: "v1", XrayVersion: "25.1.1", XrayUptimeSec: &xrayUp, ActiveUsers: &users}); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchNodeHeartbeat(ctx, n.ID, model.NodeHeartbeat{AgentVersion: "v1", AgentUptimeSec: 5}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNodeByCN(ctx, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveUsers != nil || got.XrayUptimeSec != nil || got.XrayVersion != nil {
		t.Fatalf("unknown values must be NULL, not stale or zero: users=%v uptime=%v xray=%v", got.ActiveUsers, got.XrayUptimeSec, got.XrayVersion)
	}
}

func TestTouchNodeSyncKeepsVersion(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := &model.Node{CertCN: "node-1", Name: "n1"}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	mkUser(t, s)
	before, err := s.GetNodeByCN(ctx, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchNodeSync(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNodeByCN(ctx, "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSyncAt == nil {
		t.Fatal("last_sync_at not set")
	}
	if got.UsersVersion != before.UsersVersion {
		t.Fatalf("sync mark bumped version %d -> %d", before.UsersVersion, got.UsersVersion)
	}
}
