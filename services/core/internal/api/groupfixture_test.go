package api

import (
	"context"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func idsOf(id *int64) []int64 {
	if id == nil {
		return nil
	}
	return []int64{*id}
}

func sharedGroup(t testing.TB, svc *service.Service) *int64 {
	t.Helper()
	groups, err := svc.ListGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.Name == store.TestSharedGroup {
			return &g.ID
		}
	}
	t.Fatal("no shared test group")
	return nil
}
