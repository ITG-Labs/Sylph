package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
)

func userWithDevices(t *testing.T, e *apiEnv, tg int64, hwids ...string) *model.User {
	t.Helper()
	ctx := context.Background()
	u, _, err := e.svc.RegisterUser(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	limit := 5
	p := &model.Plan{Name: fmt.Sprintf("dev-%d", tg), PriceRub: 100, PeriodDays: 30, DeviceLimit: &limit}
	if err := e.svc.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.HandlePayment(ctx, fmt.Sprintf("manual-%d", tg), u.ID, p.ID, 100); err != nil {
		t.Fatal(err)
	}
	for i, h := range hwids {
		if _, err := e.svc.Subscription(ctx, service.SubscriptionRequest{SubToken: u.SubToken, HWID: h, UserAgent: "Happ/3.6.1", DeviceOS: "iOS 18.2", DeviceModel: fmt.Sprintf("iPhone %d", 15+i)}); err != nil {
			t.Fatal(err)
		}
	}
	return u
}

func TestUserDevicesListAndRemove(t *testing.T) {
	e := newAPIEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	u := userWithDevices(t, e, 6100, "hwid-secret-a", "hwid-secret-b")
	other := userWithDevices(t, e, 6101, "hwid-secret-c")
	r := adminCall(t, bot, http.MethodGet, fmt.Sprintf("%s/v1/users/%d/devices", e.ts.URL, u.ID), nil)
	mustStatus(t, r, http.StatusOK)
	if strings.Contains(string(r.body), "hwid-secret") || strings.Contains(string(r.body), "fingerprint") {
		t.Fatalf("devices leak the HWID: %s", r.body)
	}
	var list []map[string]any
	r.decode(t, &list)
	if len(list) != 2 || list[0]["model"] != "iPhone 15" || list[0]["os"] != "iOS 18.2" || list[0]["user_agent"] != "Happ/3.6.1" || list[0]["first_seen"] == nil || list[0]["last_seen"] == nil {
		t.Fatalf("devices = %v", list)
	}
	var theirs []map[string]any
	adminCall(t, bot, http.MethodGet, fmt.Sprintf("%s/v1/users/%d/devices", e.ts.URL, other.ID), nil).decode(t, &theirs)
	mustStatus(t, adminCall(t, bot, http.MethodDelete, fmt.Sprintf("%s/v1/users/%d/devices/%v", e.ts.URL, u.ID, theirs[0]["id"]), nil), http.StatusNotFound)
	mustStatus(t, adminCall(t, bot, http.MethodDelete, fmt.Sprintf("%s/v1/users/%d/devices/%v", e.ts.URL, u.ID, list[0]["id"]), nil), http.StatusOK)
	mustStatus(t, adminCall(t, bot, http.MethodDelete, fmt.Sprintf("%s/v1/users/%d/devices/%v", e.ts.URL, u.ID, list[0]["id"]), nil), http.StatusNotFound)
	mustStatus(t, adminCall(t, bot, http.MethodDelete, fmt.Sprintf("%s/v1/users/%d/devices/abc", e.ts.URL, u.ID), nil), http.StatusBadRequest)
	var left []map[string]any
	adminCall(t, bot, http.MethodGet, fmt.Sprintf("%s/v1/users/%d/devices", e.ts.URL, u.ID), nil).decode(t, &left)
	if len(left) != 1 || left[0]["id"] != list[1]["id"] {
		t.Fatalf("left = %v", left)
	}
	mustStatus(t, adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/users/424242/devices", nil), http.StatusNotFound)
	if strings.Contains(e.logs.String(), "hwid-secret") {
		t.Fatal("HWID leaked into logs")
	}
	if !strings.Contains(e.logs.String(), `"msg":"device_removed"`) {
		t.Fatalf("no device_removed log: %s", e.logs.String())
	}
}

func TestUserDevicesEmptyListIsAnArray(t *testing.T) {
	e := newAPIEnv(t)
	u := userWithDevices(t, e, 6102)
	r := adminCall(t, e.client(t, "bot", mtls.RoleBot), http.MethodGet, fmt.Sprintf("%s/v1/users/%d/devices", e.ts.URL, u.ID), nil)
	mustStatus(t, r, http.StatusOK)
	if strings.TrimSpace(string(r.body)) != "[]" {
		t.Fatalf("devices = %s", r.body)
	}
}
