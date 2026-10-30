package service

import (
	"context"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func testGroup(t testing.TB, st *store.Store) *int64 {
	t.Helper()
	g, err := st.CreateLocationGroup(context.Background(), "g-"+model.NewToken()[:12])
	if err != nil {
		t.Fatal(err)
	}
	return &g.ID
}
