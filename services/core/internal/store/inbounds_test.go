package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkNode(t *testing.T, s *Store, cn string) *model.Node {
	t.Helper()
	n := &model.Node{CertCN: cn, Name: cn, Address: cn + ".example.com"}
	if err := s.UpsertNode(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	return n
}

func realityInbound(nodeID int64, tag string) *model.NodeInbound {
	return &model.NodeInbound{
		NodeID:    nodeID,
		Tag:       tag,
		Port:      443,
		Protocol:  "vless",
		Transport: "tcp",
		Security:  "reality",
		Label:     "REALITY",
		Settings:  json.RawMessage(`{"dest":"www.example.com:443","server_names":["www.example.com"],"short_ids":["0123456789abcdef"],"fingerprint":"chrome","private_key":"k"}`),
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya)
}

func TestInboundCRUD(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := mkNode(t, s, "node-1")
	in := realityInbound(n.ID, "reality-tcp")
	if err := s.CreateInbound(ctx, in); err != nil {
		t.Fatal(err)
	}
	if in.ID == 0 || in.CreatedAt.IsZero() {
		t.Fatalf("created = %+v", in)
	}
	got, err := s.GetInbound(ctx, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeID != n.ID || got.Tag != "reality-tcp" || got.Port != 443 || got.Protocol != "vless" ||
		got.Transport != "tcp" || got.Security != "reality" || got.Label != "REALITY" || !sameJSON(t, got.Settings, in.Settings) {
		t.Fatalf("got %+v settings %s", got, got.Settings)
	}
	got.Label = "Main"
	got.Settings = json.RawMessage(`{"dest":"www.other.com:443"}`)
	if err := s.UpdateInbound(ctx, got); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListNodeInbounds(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Label != "Main" || !sameJSON(t, list[0].Settings, []byte(`{"dest":"www.other.com:443"}`)) {
		t.Fatalf("list = %+v", list)
	}
	if err := s.DeleteInbound(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetInbound(ctx, in.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted: %v, want ErrNotFound", err)
	}
	if err := s.DeleteInbound(ctx, in.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v, want ErrNotFound", err)
	}
	if err := s.UpdateInbound(ctx, got); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update deleted: %v, want ErrNotFound", err)
	}
}

func TestInboundListOrderedByTag(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := mkNode(t, s, "node-1")
	for _, tag := range []string{"b", "a", "c"} {
		if err := s.CreateInbound(ctx, realityInbound(n.ID, tag)); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListNodeInbounds(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Tag != "a" || list[1].Tag != "b" || list[2].Tag != "c" {
		t.Fatalf("order = %+v", list)
	}
}

func TestInboundTagUniquePerNode(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n1, n2 := mkNode(t, s, "node-1"), mkNode(t, s, "node-2")
	if err := s.CreateInbound(ctx, realityInbound(n1.ID, "main")); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateInbound(ctx, realityInbound(n1.ID, "main")); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate tag: %v, want ErrConflict", err)
	}
	if err := s.CreateInbound(ctx, realityInbound(n2.ID, "main")); err != nil {
		t.Fatalf("same tag on another node: %v", err)
	}
	other := realityInbound(n1.ID, "second")
	if err := s.CreateInbound(ctx, other); err != nil {
		t.Fatal(err)
	}
	other.Tag = "main"
	if err := s.UpdateInbound(ctx, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("rename onto taken tag: %v, want ErrConflict", err)
	}
}

func TestInboundForUnknownNode(t *testing.T) {
	s := setupStore(t)
	if err := s.CreateInbound(context.Background(), realityInbound(424242, "main")); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("unknown node: %v, want ErrInvalidReference", err)
	}
}

func TestInboundChangesBumpOnlyItsNode(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n1 := mkNode(t, s, "node-1")
	mkNode(t, s, "node-2")
	v1, v2 := nodeVersion(t, s, "node-1"), nodeVersion(t, s, "node-2")
	in := realityInbound(n1.ID, "main")
	if err := s.CreateInbound(ctx, in); err != nil {
		t.Fatal(err)
	}
	afterCreate := nodeVersion(t, s, "node-1")
	if afterCreate <= v1 {
		t.Fatalf("create did not bump node-1: %d -> %d", v1, afterCreate)
	}
	if got := nodeVersion(t, s, "node-2"); got != v2 {
		t.Fatalf("create bumped node-2: %d -> %d", v2, got)
	}
	in.Settings = json.RawMessage(`{"dest":"www.example.com:443","server_names":["www.example.com"],"short_ids":["0123456789abcdef"],"fingerprint":"firefox","private_key":"k"}`)
	if err := s.UpdateInbound(ctx, in); err != nil {
		t.Fatal(err)
	}
	afterUpdate := nodeVersion(t, s, "node-1")
	if afterUpdate <= afterCreate {
		t.Fatalf("update did not bump: %d -> %d", afterCreate, afterUpdate)
	}
	in.Settings = json.RawMessage(`{ "private_key":"k", "fingerprint":"firefox", "short_ids":["0123456789abcdef"], "server_names":["www.example.com"], "dest":"www.example.com:443" }`)
	if err := s.UpdateInbound(ctx, in); err != nil {
		t.Fatal(err)
	}
	if got := nodeVersion(t, s, "node-1"); got != afterUpdate {
		t.Fatalf("equivalent update bumped: %d -> %d", afterUpdate, got)
	}
	if err := s.DeleteInbound(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	if got := nodeVersion(t, s, "node-1"); got <= afterUpdate {
		t.Fatalf("delete did not bump: %d -> %d", afterUpdate, got)
	}
	if got := nodeVersion(t, s, "node-2"); got != v2 {
		t.Fatalf("node-2 moved: %d -> %d", v2, got)
	}
}
