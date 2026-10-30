package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestCreateNode(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g, err := s.CreateLocationGroup(ctx, "premium")
	if err != nil {
		t.Fatal(err)
	}
	n := &model.Node{CertCN: "node-de-1", Name: "Germany", Address: "de1.example.com", Enabled: true}
	if err := s.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeGroups(ctx, n.ID, []int64{g.ID}); err != nil {
		t.Fatal(err)
	}
	if n.ID == 0 || n.CreatedAt.IsZero() {
		t.Fatalf("created = %+v", n)
	}
	got, err := s.GetNode(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CertCN != "node-de-1" || got.Name != "Germany" || len(got.GroupIDs) != 1 || got.GroupIDs[0] != g.ID ||
		got.Address != "de1.example.com" || !got.Enabled || got.ConfigError != nil {
		t.Fatalf("got %+v", got)
	}
	if _, err := s.GetNode(ctx, 424242); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v, want ErrNotFound", err)
	}
}

func TestCreateNodeConflictAndUnknownGroup(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	if err := s.CreateNode(ctx, &model.Node{CertCN: "node-1", Name: "a", Address: "a.example.com", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNode(ctx, &model.Node{CertCN: "node-1", Name: "b", Address: "b.example.com", Enabled: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate cert_cn: %v, want ErrConflict", err)
	}
	n := &model.Node{CertCN: "node-2", Name: "c", Address: "c.example.com", Enabled: true}
	if err := s.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeGroups(ctx, n.ID, []int64{424242}); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("unknown group: %v, want ErrInvalidReference", err)
	}
}

func TestUpdateNode(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	g, err := s.CreateLocationGroup(ctx, "premium")
	if err != nil {
		t.Fatal(err)
	}
	n := &model.Node{CertCN: "node-1", Name: "a", Address: "a.example.com", Enabled: true}
	if err := s.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	n.Name, n.Address, n.Enabled = "renamed", "b.example.com", false
	if err := s.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeGroups(ctx, n.ID, []int64{g.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNode(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" || got.Address != "b.example.com" || got.Enabled || len(got.GroupIDs) != 1 || got.GroupIDs[0] != g.ID {
		t.Fatalf("got %+v", got)
	}
	if err := s.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	again, err := s.GetNode(ctx, n.ID)
	if err != nil || again.UsersVersion != n.UsersVersion {
		t.Fatalf("update returned version %d, stored %+v, %v", n.UsersVersion, again, err)
	}
	ghost := &model.Node{ID: 424242, CertCN: "x", Name: "x", Address: "x.example.com"}
	if err := s.UpdateNode(ctx, ghost); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown node: %v, want ErrNotFound", err)
	}
}

func TestHeartbeatStoresConfigError(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := mkNode(t, s, "node-1")
	msg := "xray_config_rejected: xray did not come back"
	if err := s.TouchNodeHeartbeat(ctx, n.ID, model.NodeHeartbeat{AgentVersion: "v1", ConfigError: &msg}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNode(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigError == nil || *got.ConfigError != msg {
		t.Fatalf("config_error = %v", got.ConfigError)
	}
	if err := s.TouchNodeHeartbeat(ctx, n.ID, model.NodeHeartbeat{AgentVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetNode(ctx, n.ID)
	if got.ConfigError != nil {
		t.Fatalf("config_error not cleared: %v", *got.ConfigError)
	}
}
