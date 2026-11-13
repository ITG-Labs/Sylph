package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestClientRoutingDefaults(t *testing.T) {
	s := setupStore(t)
	cr, err := s.GetClientRouting(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(cr.Rules) != "[]" || cr.Revision != 0 || cr.UpdatedAt != nil {
		t.Fatalf("defaults = %s rev=%d updated=%v", cr.Rules, cr.Revision, cr.UpdatedAt)
	}
}

func TestPutClientRoutingRoundTrip(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	rules := json.RawMessage(`[{"name":"ru","action":"direct","domain":["geosite:category-ru"]}]`)
	put, err := s.PutClientRouting(ctx, rules, 0)
	if err != nil {
		t.Fatal(err)
	}
	if put.Revision != 1 || put.UpdatedAt == nil {
		t.Fatalf("put = rev %d updated %v", put.Revision, put.UpdatedAt)
	}
	got, err := s.GetClientRouting(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || got.UpdatedAt == nil || !got.UpdatedAt.Equal(*put.UpdatedAt) || !jsonEqual(t, got.Rules, rules) {
		t.Fatalf("get = %s rev=%d", got.Rules, got.Revision)
	}
}

func TestPutClientRoutingStaleRevision(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	first := json.RawMessage(`[{"name":"a","action":"block","port":"25"}]`)
	if _, err := s.PutClientRouting(ctx, first, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutClientRouting(ctx, json.RawMessage(`[]`), 0); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("want ErrStaleRevision, got %v", err)
	}
	got, err := s.GetClientRouting(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || !jsonEqual(t, got.Rules, first) {
		t.Fatalf("stale write changed state: %s rev=%d", got.Rules, got.Revision)
	}
}

func TestClientGeoTagsRoundTrip(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	got, err := s.GetClientGeoTags(ctx)
	if err != nil || got != nil {
		t.Fatalf("before put: %s, %v", got, err)
	}
	tags := json.RawMessage(`{"release":"202611131125","geosite":["category-ru"],"geoip":["ru"],"fetched_at":"2026-11-13T11:25:00Z"}`)
	if err := s.PutClientGeoTags(ctx, tags); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetClientGeoTags(ctx)
	if err != nil || !jsonEqual(t, got, tags) {
		t.Fatalf("after put: %s, %v", got, err)
	}
}

func TestMigration024DownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	m, closeDB, err := newMigrator(pool)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := m.Migrate(23); err != nil {
		t.Fatalf("down to 023: %v", err)
	}
	for _, c := range []string{"client_rules", "client_rules_revision", "client_rules_updated_at", "client_geo_tags"} {
		if columnExists(t, pool, "settings", c) {
			t.Fatalf("settings.%s survived down 024", c)
		}
	}
	if err := m.Migrate(24); err != nil {
		t.Fatalf("up to 024: %v", err)
	}
	if !columnExists(t, pool, "settings", "client_rules") {
		t.Fatal("client_rules missing after up")
	}
}

func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}
