package panel

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var coreRouteRe = regexp.MustCompile(`(?m)s\.route\(mux, "([A-Z]+ [^"]+)", .*?((?:mtls\.Role\w+(?:, )?)+)\)\s*$`)

func coreRoutes(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "core", "internal", "api", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("core api sources not found: %v", err)
	}
	out := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range coreRouteRe.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = strings.Contains(m[2], "mtls.RolePanel")
		}
	}
	if len(out) < 40 {
		t.Fatalf("parsed only %d core routes", len(out))
	}
	return out
}

func TestAllowlistCoversEveryAdminRoute(t *testing.T) {
	core := coreRoutes(t)
	allowed := map[string]bool{}
	for _, r := range coreAllowlist {
		allowed[r.method+" "+r.pattern] = true
	}
	for route := range core {
		if strings.Contains(route, " /v1/admin/") && !allowed[route] {
			t.Errorf("admin route %s is not in the panel allowlist", route)
		}
	}
	for route := range allowed {
		panelOK, exists := core[route]
		if !exists {
			t.Errorf("allowlisted %s does not exist in core", route)
		} else if !panelOK {
			t.Errorf("allowlisted %s is not open to the panel role in core", route)
		}
	}
}

func TestAllowlistOutsideAdminIsExactlyCardAndManualPayment(t *testing.T) {
	var got []string
	for _, r := range coreAllowlist {
		if !strings.HasPrefix(r.pattern, "/v1/admin/") {
			got = append(got, r.method+" "+r.pattern)
		}
	}
	slices.Sort(got)
	want := []string{
		"DELETE /v1/users/{user_id}/devices/{device_id}",
		"GET /v1/users/{user_id}/stats",
		"POST /v1/payments",
		"POST /v1/users/{user_id}/devices/reset",
		"POST /v1/users/{user_id}/rotate",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("non-admin allowlist:\n got %v\nwant %v", got, want)
	}
}

func TestMatchRoute(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         string
	}{
		{"GET", "/v1/admin/users", "GET /v1/admin/users"},
		{"GET", "/v1/admin/users/42", "GET /v1/admin/users/{id}"},
		{"POST", "/v1/admin/users/42/ban", "POST /v1/admin/users/{id}/ban"},
		{"GET", "/v1/admin/payments/2f6a-11.x_Y", "GET /v1/admin/payments/{payment_id}"},
		{"POST", "/v1/admin/payments/manual-20261013-ab12/refund", "POST /v1/admin/payments/{payment_id}/refund"},
		{"PUT", "/v1/admin/plans/3/access/123456789", "PUT /v1/admin/plans/{id}/access/{telegram_id}"},
		{"DELETE", "/v1/users/7/devices/9", "DELETE /v1/users/{user_id}/devices/{device_id}"},
		{"POST", "/v1/payments", "POST /v1/payments"},
		{"GET", "/v1/admin/users/", ""},
		{"GET", "/v1/admin/users/abc", ""},
		{"GET", "/v1/admin/users/-1", ""},
		{"GET", "/v1/admin/users/12345678901234567890", ""},
		{"GET", "/v1/admin/users/42/", ""},
		{"GET", "/v1/admin//users", ""},
		{"POST", "/v1/admin/users/42", ""},
		{"GET", "/v1/admin/payments/..", ""},
		{"GET", "/v1/admin/payments/.", ""},
		{"POST", "/v1/admin/payments/../users/1/ban", ""},
		{"GET", "/v1/admin/payments/" + strings.Repeat("a", 65), ""},
		{"GET", "/v1/admin/payments/a%2Fb", ""},
		{"GET", "/v1/admin/users/4%32", ""},
		{"POST", "/v1/sync/users", ""},
		{"POST", "/v1/subscription", ""},
		{"POST", "/v1/users/by-token", ""},
		{"POST", "/v1/payments/create", ""},
		{"POST", "/v1/maintenance/expire", ""},
		{"POST", "/v1/users/7/trial", ""},
		{"GET", "/v1/users/7", ""},
		{"GET", "/health", ""},
		{"GET", "v1/admin/users", ""},
		{"GET", "", ""},
	} {
		r, path, ok := matchRoute(tc.method, tc.path)
		got := ""
		if ok {
			got = r.method + " " + r.pattern
			if path != tc.path {
				t.Errorf("%s %s: rebuilt as %s", tc.method, tc.path, path)
			}
		}
		if got != tc.want {
			t.Errorf("%s %s: got %q want %q", tc.method, tc.path, got, tc.want)
		}
	}
}
