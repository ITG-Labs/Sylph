package api

import (
	"net/http"
	"testing"
)

func TestMaintenanceCleanup(t *testing.T) {
	e := newAdminEnv(t)
	mustStatus(t, e.call(t, http.MethodPost, "/v1/maintenance/cleanup", map[string]any{"older_than_days": "30"}), http.StatusBadRequest)
	for _, days := range []any{0, -1, 3651} {
		mustField(t, e.call(t, http.MethodPost, "/v1/maintenance/cleanup", map[string]any{"older_than_days": days}), http.StatusBadRequest, "bad_request", "older_than_days")
	}
	mustField(t, e.call(t, http.MethodPost, "/v1/maintenance/cleanup", map[string]any{}), http.StatusBadRequest, "bad_request", "older_than_days")
	r := e.call(t, http.MethodPost, "/v1/maintenance/cleanup", map[string]any{"older_than_days": 30})
	mustStatus(t, r, http.StatusOK)
	var got map[string]int64
	r.decode(t, &got)
	if len(got) != 3 || got["deltas"] != 0 || got["batches"] != 0 || got["keys"] != 0 {
		t.Fatalf("cleanup = %v", got)
	}
}
