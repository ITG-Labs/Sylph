package api

import (
	"context"
	"testing"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
)

func idsOf(id *int64) []int64 {
	if id == nil {
		return nil
	}
	return []int64{*id}
}

func testGroup(t testing.TB, svc *service.Service) *int64 {
	t.Helper()
	g, err := svc.CreateGroup(context.Background(), "g-"+model.NewToken()[:12])
	if err != nil {
		t.Fatal(err)
	}
	return &g.ID
}
