package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"

	"github.com/ITG-Labs/sylph/pkg/clientroute"
)

func TestAdminClientRouting(t *testing.T) {
	e := newAdminEnv(t)
	r := e.call(t, http.MethodGet, "/v1/admin/client-routing", nil)
	mustStatus(t, r, http.StatusOK)
	var v struct {
		Rules    []clientroute.Rule `json:"rules"`
		Revision int64              `json:"revision"`
		Geo      struct {
			Loaded bool `json:"loaded"`
		} `json:"geo"`
		Issues [][]clientroute.Issue `json:"issues"`
	}
	r.decode(t, &v)
	if len(v.Rules) != 0 || v.Revision != 0 || v.Geo.Loaded {
		t.Fatalf("empty = %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPut, "/v1/admin/client-routing", map[string]any{"rules": []any{map[string]any{"name": "a", "action": "direct", "domain": []string{"geosite:category-ru"}}}, "revision": 0}), http.StatusBadRequest, "bad_request", "rules[0].domain")
	if _, err := e.svc.PutClientGeoTags(context.Background(), clientroute.Tags{Release: "r1", Geosite: []string{"category-ru"}, Geoip: []string{"ru"}}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"rules": []any{
		map[string]any{"name": "ru", "action": "direct", "domain": []string{"geosite:category-ru"}},
		map[string]any{"name": "smtp", "action": "block", "port": "25"},
	}, "revision": 0}
	r = e.call(t, http.MethodPut, "/v1/admin/client-routing", body)
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &v)
	if v.Revision != 1 || len(v.Rules) != 2 || !v.Geo.Loaded || len(v.Issues[1]) != 2 {
		t.Fatalf("saved = %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPut, "/v1/admin/client-routing", body), http.StatusConflict, "conflict", "revision")
	mustField(t, e.call(t, http.MethodPut, "/v1/admin/client-routing", map[string]any{"rules": "nope", "revision": 1}), http.StatusBadRequest, "bad_request", "")
	r = e.call(t, http.MethodGet, "/v1/admin/client-routing/geo-tags", nil)
	mustStatus(t, r, http.StatusOK)
	var tags clientroute.Tags
	r.decode(t, &tags)
	if tags.Release != "r1" || len(tags.Geosite) != 1 {
		t.Fatalf("tags = %s", r.body)
	}
}

func TestAdminClientGeoTagsNotLoaded(t *testing.T) {
	e := newAdminEnv(t)
	r := e.call(t, http.MethodGet, "/v1/admin/client-routing/geo-tags", nil)
	mustStatus(t, r, http.StatusOK)
	if string(r.body) != "{\"release\":\"\",\"geosite\":[],\"geoip\":[],\"fetched_at\":\"0001-01-01T00:00:00Z\",\"changed_at\":\"0001-01-01T00:00:00Z\"}\n" {
		t.Fatalf("body = %s", r.body)
	}
}

func TestMaintenanceGeoTagsAcceptsLargeLists(t *testing.T) {
	e := newAPIEnv(t)
	cron := e.client(t, "cron", mtls.RoleCron)
	geosite := make([]string, 5000)
	for i := range geosite {
		geosite[i] = fmt.Sprintf("category-long-name-%05d", i)
	}
	raw, _ := json.Marshal(clientroute.Tags{Release: "r", Geosite: geosite, Geoip: []string{"ru"}})
	resp, err := cron.Post(e.ts.URL+"/v1/maintenance/geo-tags", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
}
