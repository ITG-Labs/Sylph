package store

import (
	"context"
	"testing"
)

func sharedGroup(t testing.TB, s *Store) *int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(context.Background(), `SELECT id FROM location_groups WHERE name = $1`, TestSharedGroup).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return &id
}
