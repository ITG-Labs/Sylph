package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func TestUserStatsEndpoint(t *testing.T) {
	ts, client, svc := newAPITestEnv(t, mtls.RoleBot)
	ctx := context.Background()
	u, key, err := svc.RegisterUser(ctx, 950)
	if err != nil {
		t.Fatal(err)
	}
	p := &model.Plan{Name: "m", PriceRub: 100, PeriodDays: 30}
	if err := svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenewSubscription(ctx, u.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpsertNode(ctx, &model.Node{CertCN: "de-1", Name: "🇩🇪 Германия"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordNodeDeltas(ctx, "de-1", "batch-1", []model.TrafficDelta{{KeyUUID: key, BytesUp: 10, BytesDown: 90}}); err != nil {
		t.Fatal(err)
	}
	var st struct {
		PeriodID *int64 `json:"period_id"`
		Nodes    []struct {
			Name   string `json:"name"`
			Today  int64  `json:"today"`
			Days7  int64  `json:"days_7"`
			Days30 int64  `json:"days_30"`
			Period int64  `json:"period"`
		} `json:"nodes"`
	}
	if code := getJSON(t, client, fmt.Sprintf("%s/v1/users/%d/stats", ts.URL, u.ID), &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.PeriodID == nil || len(st.Nodes) != 1 || st.Nodes[0].Name != "🇩🇪 Германия" || st.Nodes[0].Today != 100 || st.Nodes[0].Period != 100 {
		t.Fatalf("stats = %+v", st)
	}
	if code := getJSON(t, client, ts.URL+"/v1/users/424242/stats", nil); code != http.StatusNotFound {
		t.Fatalf("unknown user = %d", code)
	}
	if code := getJSON(t, client, ts.URL+"/v1/users/x/stats", nil); code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", code)
	}
}
