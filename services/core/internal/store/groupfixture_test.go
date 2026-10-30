package store

import (
	"context"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func testGroup(t testing.TB, s *Store) *int64 {
	t.Helper()
	g, err := s.CreateLocationGroup(context.Background(), "g-"+model.NewToken()[:12])
	if err != nil {
		t.Fatal(err)
	}
	return &g.ID
}
