package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/clientroute"
)

func loadTags(t *testing.T, svc *Service, geosite, geoip []string) {
	t.Helper()
	if _, err := svc.PutClientGeoTags(context.Background(), clientroute.Tags{Release: "r1", Geosite: geosite, Geoip: geoip}); err != nil {
		t.Fatal(err)
	}
}

func TestGetClientRoutingEmpty(t *testing.T) {
	svc := setupService(t)
	v, err := svc.GetClientRouting(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Rules == nil || len(v.Rules) != 0 || v.Issues == nil || v.Missing == nil || v.Overlaps == nil || v.Geo.Loaded || v.Revision != 0 {
		t.Fatalf("view = %+v", v)
	}
	raw, _ := json.Marshal(v)
	for _, key := range []string{`"rules":[]`, `"issues":[]`, `"missing":[]`, `"overlaps":[]`} {
		if !json.Valid(raw) || !strings.Contains(string(raw), key) {
			t.Fatalf("json %s lacks %s", raw, key)
		}
	}
}

func TestPutClientRoutingValid(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	loadTags(t, svc, []string{"category-ru"}, []string{"ru"})
	v, err := svc.PutClientRouting(ctx, []clientroute.Rule{
		{Name: "ru", Action: clientroute.ActionDirect, Domain: []string{"GEOSITE:category-ru"}},
		{Name: "smtp", Action: clientroute.ActionBlock, Port: "25"},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v.Revision != 1 || v.UpdatedAt == nil || v.Rules[0].Domain[0] != "geosite:category-ru" {
		t.Fatalf("view = %+v", v)
	}
	if !slices.Contains(v.Issues[1], clientroute.Issue{Client: clientroute.Happ, Skip: true, Reason: "port"}) || len(v.Issues[0]) != 0 {
		t.Fatalf("issues = %+v", v.Issues)
	}
	if !v.Geo.Loaded || v.Geo.Release != "r1" || v.Geo.FetchedAt == nil {
		t.Fatalf("geo = %+v", v.Geo)
	}
	got, err := svc.GetClientRouting(ctx)
	if err != nil || len(got.Rules) != 2 || got.Revision != 1 {
		t.Fatalf("get = %+v, %v", got, err)
	}
}

func TestPutClientRoutingInvalidField(t *testing.T) {
	svc := setupService(t)
	_, err := svc.PutClientRouting(context.Background(), []clientroute.Rule{{Name: "a", Action: clientroute.ActionDirect, Network: "tcp,udp", Port: "1"}}, 0)
	mustField(t, err, "rules[0].network")
}

func TestPutClientRoutingUnknownNode(t *testing.T) {
	svc := setupService(t)
	_, err := svc.PutClientRouting(context.Background(), []clientroute.Rule{{Name: "a", Action: clientroute.ActionNode, NodeID: 999999, Domain: []string{"a.ru"}}}, 0)
	mustField(t, err, "rules[0].node_id")
}

func TestPutClientRoutingConflict(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	rules := []clientroute.Rule{{Name: "a", Action: clientroute.ActionBlock, Port: "25"}}
	if _, err := svc.PutClientRouting(ctx, rules, 0); err != nil {
		t.Fatal(err)
	}
	_, err := svc.PutClientRouting(ctx, nil, 0)
	mustConflict(t, err, "revision")
}

func TestPutClientGeoTagsFilters(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	changed, err := svc.PutClientGeoTags(ctx, clientroute.Tags{Release: "r1", Geosite: []string{"youtube", "Bad Tag", "category-ru", "youtube"}, Geoip: []string{"ru"}})
	if err != nil || !changed {
		t.Fatalf("first put: %v %v", changed, err)
	}
	tags, err := svc.ClientGeoTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tags.Geosite, []string{"category-ru", "youtube"}) || time.Since(tags.FetchedAt) > time.Minute {
		t.Fatalf("tags = %+v", tags)
	}
	changed, err = svc.PutClientGeoTags(ctx, clientroute.Tags{Release: "r1", Geosite: []string{"category-ru", "youtube"}, Geoip: []string{"ru"}})
	if err != nil || changed {
		t.Fatalf("same put: changed=%v err=%v", changed, err)
	}
	if _, err := svc.PutClientGeoTags(ctx, clientroute.Tags{Release: "r2", Geosite: []string{"Bad Tag"}, Geoip: []string{"ru"}}); err == nil {
		t.Fatal("empty geosite after filtering must fail")
	}
}

func TestClientGeoTagsNotLoaded(t *testing.T) {
	svc := setupService(t)
	tags, err := svc.ClientGeoTags(context.Background())
	if err != nil || tags != nil {
		t.Fatalf("tags = %+v, %v", tags, err)
	}
}

func TestClientRoutingMissingAfterTagsShrink(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	ctx := context.Background()
	loadTags(t, f.svc, []string{"x", "y"}, []string{"ru"})
	if _, err := f.svc.PutClientRouting(ctx, []clientroute.Rule{
		{Name: "x", Action: clientroute.ActionDirect, Domain: []string{"geosite:x"}},
		{Name: "y", Action: clientroute.ActionDirect, Domain: []string{"geosite:y"}},
	}, 0); err != nil {
		t.Fatal(err)
	}
	loadTags(t, f.svc, []string{"y"}, []string{"ru"})
	v, err := f.svc.GetClientRouting(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(v.Missing[0], []string{"geosite:x"}) || len(v.Missing[1]) != 0 {
		t.Fatalf("missing = %+v", v.Missing)
	}
	feed := f.view(t, "hw-1").ClientRouting
	if feed == nil || len(feed.Rules) != 1 || feed.Rules[0].Name != "y" {
		t.Fatalf("feed = %+v", feed)
	}
}

func TestSubscriptionClientRoutingFeed(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	ctx := context.Background()
	n := f.node(t, "berlin", &f.group.ID, true, "main")
	if _, err := f.svc.PutClientRouting(ctx, []clientroute.Rule{{Name: "ai", Action: clientroute.ActionNode, NodeID: n.ID, Domain: []string{"openai.com"}}}, 0); err != nil {
		t.Fatal(err)
	}
	v := f.view(t, "hw-1")
	if v.ClientRouting == nil || len(v.ClientRouting.Rules) != 1 || v.ClientRouting.UpdatedAt.IsZero() || v.ClientRouting.Rules[0].NodeID != n.ID {
		t.Fatalf("feed = %+v", v.ClientRouting)
	}
	if len(v.Connections) == 0 || v.Connections[0].NodeID != n.ID {
		t.Fatalf("connections = %+v", v.Connections)
	}
}

func TestSubscriptionClientRoutingOnExpired(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	if _, err := svc.PutClientRouting(ctx, []clientroute.Rule{{Name: "a", Action: clientroute.ActionBlock, Port: "25"}}, 0); err != nil {
		t.Fatal(err)
	}
	u, _, err := svc.RegisterUser(ctx, 901)
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.Subscription(ctx, SubscriptionRequest{SubToken: u.SubToken, UserAgent: "Happ/1"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusExpired || v.ClientRouting == nil || len(v.ClientRouting.Rules) != 1 {
		t.Fatalf("view = %+v", v)
	}
}

func TestSubscriptionNoClientRouting(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	raw, err := json.Marshal(f.view(t, "hw-1"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "client_routing") {
		t.Fatalf("empty set must not be sent: %s", raw)
	}
}
