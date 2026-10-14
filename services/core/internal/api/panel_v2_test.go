package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

type userListPage struct {
	Users []struct {
		ID               int64   `json:"id"`
		TelegramUsername *string `json:"telegram_username"`
		TelegramName     *string `json:"telegram_name"`
	} `json:"users"`
}

func TestUserRegistrationKeepsTheTelegramProfile(t *testing.T) {
	e := newAdminEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	register := func(body any) adminResp {
		return adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users", body)
	}
	r := register(map[string]any{"telegram_id": 8101, "profile": map[string]any{"username": "Ivan_P", "first_name": "Иван", "last_name": "Петров"}})
	mustStatus(t, r, http.StatusOK)
	var u struct {
		ID int64 `json:"id"`
	}
	r.decode(t, &u)
	find := func(q string) userListPage {
		r := e.call(t, http.MethodGet, "/v1/admin/users?q="+q, nil)
		mustStatus(t, r, http.StatusOK)
		var page userListPage
		r.decode(t, &page)
		return page
	}
	page := find("@ivan")
	if len(page.Users) != 1 || page.Users[0].ID != u.ID || *page.Users[0].TelegramUsername != "Ivan_P" || *page.Users[0].TelegramName != "Иван Петров" {
		t.Fatalf("by nick = %+v", page)
	}
	if page := find("%D0%BF%D0%B5%D1%82%D1%80"); len(page.Users) != 1 {
		t.Fatalf("by name = %+v", page)
	}
	mustStatus(t, register(map[string]any{"telegram_id": 8101}), http.StatusOK)
	if page := find("@ivan"); len(page.Users) != 1 {
		t.Fatal("registration without a profile dropped the nick")
	}
	mustStatus(t, register(map[string]any{"telegram_id": 8101, "profile": map[string]any{"first_name": "Ваня\n"}}), http.StatusOK)
	r = e.call(t, http.MethodGet, fmt.Sprintf("/v1/admin/users/%d", u.ID), nil)
	mustStatus(t, r, http.StatusOK)
	var card struct {
		Telegram *struct {
			Username  *string `json:"username"`
			Name      string  `json:"name"`
			UpdatedAt string  `json:"updated_at"`
		} `json:"telegram"`
	}
	r.decode(t, &card)
	if card.Telegram == nil || card.Telegram.Username != nil || card.Telegram.Name != "Ваня" || card.Telegram.UpdatedAt == "" {
		t.Fatalf("card telegram = %s", r.body)
	}
	mustField(t, register(map[string]any{"telegram_id": 8101, "profile": map[string]any{"nick": "x"}}), http.StatusBadRequest, "bad_request", "")
	if _, err := e.svc.BanUser(context.Background(), u.ID, ""); err != nil {
		t.Fatal(err)
	}
	mustStatus(t, register(map[string]any{"telegram_id": 8101, "profile": map[string]any{"username": "banned_now", "first_name": "Ваня"}}), http.StatusForbidden)
	if page := find("@banned_now"); len(page.Users) != 1 {
		t.Fatal("a banned user's profile was not kept")
	}
}

func TestAdminGroupRenameAndDelete(t *testing.T) {
	e := newAdminEnv(t)
	var g, other struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	e.call(t, http.MethodPost, "/v1/admin/groups", map[string]string{"name": "eu"}).decode(t, &g)
	e.call(t, http.MethodPost, "/v1/admin/groups", map[string]string{"name": "asia"}).decode(t, &other)
	path := fmt.Sprintf("/v1/admin/groups/%d", g.ID)
	r := e.call(t, http.MethodPatch, path, map[string]string{"name": "Европа"})
	mustStatus(t, r, http.StatusOK)
	var renamed struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	r.decode(t, &renamed)
	if renamed.ID != g.ID || renamed.Name != "Европа" {
		t.Fatalf("renamed = %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPatch, path, map[string]string{"name": "asia"}), http.StatusConflict, "conflict", "name")
	mustField(t, e.call(t, http.MethodPatch, path, map[string]string{"name": ""}), http.StatusBadRequest, "bad_request", "name")
	mustStatus(t, e.call(t, http.MethodPatch, "/v1/admin/groups/999", map[string]string{"name": "x"}), http.StatusNotFound)
	nodeID := e.createNode(t, "node-eu", &g.ID)
	mustField(t, e.call(t, http.MethodDelete, path, nil), http.StatusConflict, "conflict", "group")
	mustStatus(t, e.call(t, http.MethodPatch, fmt.Sprintf("/v1/admin/nodes/%d", nodeID), map[string]any{"group_id": other.ID}), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodDelete, path, nil), http.StatusOK)
	mustStatus(t, e.call(t, http.MethodDelete, path, nil), http.StatusNotFound)
	entries := auditList(t, e, "?entity=group")
	actions := map[string]string{}
	for _, a := range entries {
		actions[a.Action] = a.EntityID
	}
	if actions["update"] != fmt.Sprint(g.ID) || actions["delete"] != fmt.Sprint(g.ID) {
		t.Fatalf("audit = %+v", entries)
	}
}

func TestAdminNodeStats(t *testing.T) {
	e := newAdminEnv(t)
	mustStatus(t, e.call(t, http.MethodGet, "/v1/admin/nodes/999/stats", nil), http.StatusNotFound)
	nodeID := e.createNode(t, "node-stats", nil)
	r := e.call(t, http.MethodGet, fmt.Sprintf("/v1/admin/nodes/%d/stats", nodeID), nil)
	mustStatus(t, r, http.StatusOK)
	var st struct {
		Today *struct {
			Up   *int64 `json:"up"`
			Down *int64 `json:"down"`
		} `json:"today"`
		Days7  map[string]int64 `json:"days_7"`
		Days30 map[string]int64 `json:"days_30"`
		Daily  []struct {
			Day  string `json:"day"`
			Up   *int64 `json:"up"`
			Down *int64 `json:"down"`
		} `json:"daily"`
		Top map[string][]any `json:"top"`
	}
	r.decode(t, &st)
	if st.Today == nil || st.Today.Up == nil || st.Days7 == nil || st.Days30 == nil || len(st.Daily) != 30 || st.Daily[0].Day == "" || st.Daily[0].Up == nil {
		t.Fatalf("stats = %s", r.body)
	}
	for _, k := range []string{"today", "days_7", "days_30"} {
		if list, ok := st.Top[k]; !ok || list == nil {
			t.Fatalf("top %s missing: %s", k, r.body)
		}
	}
}

func TestAdminSettingsReachTheSubscription(t *testing.T) {
	e := newAdminEnv(t)
	r := e.call(t, http.MethodGet, "/v1/admin/settings", nil)
	mustStatus(t, r, http.StatusOK)
	var st struct {
		Title   string `json:"subs_title"`
		Support string `json:"subs_support_url"`
		At      string `json:"updated_at"`
	}
	r.decode(t, &st)
	if st.Title != "Sylph" || st.Support != "" || st.At == "" {
		t.Fatalf("defaults = %s", r.body)
	}
	mustField(t, e.call(t, http.MethodPatch, "/v1/admin/settings", map[string]string{"subs_support_url": "javascript:alert(1)"}), http.StatusBadRequest, "bad_request", "subs_support_url")
	mustField(t, e.call(t, http.MethodPatch, "/v1/admin/settings", map[string]string{"subs_title": ""}), http.StatusBadRequest, "bad_request", "subs_title")
	mustStatus(t, e.call(t, http.MethodPatch, "/v1/admin/settings", map[string]string{"logo": "x"}), http.StatusBadRequest)
	r = e.call(t, http.MethodPatch, "/v1/admin/settings", map[string]string{"subs_title": "Нора", "subs_support_url": "https://t.me/nora"})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &st)
	if st.Title != "Нора" || st.Support != "https://t.me/nora" {
		t.Fatalf("updated = %s", r.body)
	}
	u, _ := botUserAndPlan(t, e.apiEnv, 8201, 199)
	subs := e.client(t, "subs", mtls.RoleSubs)
	r = adminCall(t, subs, http.MethodPost, e.ts.URL+"/v1/subscription", map[string]any{"sub_token": u.SubToken})
	mustStatus(t, r, http.StatusOK)
	var view struct {
		Title   string `json:"title"`
		Support string `json:"support_url"`
	}
	r.decode(t, &view)
	if view.Title != "Нора" || view.Support != "https://t.me/nora" {
		t.Fatalf("subscription = %s", r.body)
	}
	entries := auditList(t, e, "?entity=settings")
	if len(entries) != 1 || entries[0].Action != "update" || entries[0].Details["subs_title"] != "Нора" {
		t.Fatalf("audit = %+v", entries)
	}
}

func TestAdminPlansShowActiveUsers(t *testing.T) {
	e := newAdminEnv(t)
	u, p := botUserAndPlan(t, e.apiEnv, 8301, 199)
	if _, err := e.svc.RecordPayment(context.Background(), "pay-8301", u.ID, p.ID, 199); err != nil {
		t.Fatal(err)
	}
	r := e.call(t, http.MethodGet, "/v1/admin/plans", nil)
	mustStatus(t, r, http.StatusOK)
	var list []struct {
		ID          int64 `json:"id"`
		Name        string
		ActiveUsers *int `json:"active_users"`
	}
	r.decode(t, &list)
	if len(list) != 1 || list[0].ID != p.ID || list[0].ActiveUsers == nil || *list[0].ActiveUsers != 1 {
		t.Fatalf("plans = %s", r.body)
	}
	bot := e.client(t, "bot", mtls.RoleBot)
	r = adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/plans", nil)
	mustStatus(t, r, http.StatusOK)
	var onSale []map[string]any
	r.decode(t, &onSale)
	if _, leaked := onSale[0]["active_users"]; leaked {
		t.Fatalf("bot plans carry active_users: %s", r.body)
	}
}

func TestAdminNodesCountInbounds(t *testing.T) {
	e := newAdminEnv(t)
	id := e.createNode(t, "node-count", nil)
	count := func() int {
		r := e.call(t, http.MethodGet, "/v1/admin/nodes", nil)
		mustStatus(t, r, http.StatusOK)
		var list []struct {
			ID       int64 `json:"id"`
			Inbounds *int  `json:"inbounds"`
		}
		r.decode(t, &list)
		for _, n := range list {
			if n.ID == id && n.Inbounds != nil {
				return *n.Inbounds
			}
		}
		t.Fatalf("no inbounds count: %s", r.body)
		return -1
	}
	if c := count(); c != 0 {
		t.Fatalf("fresh node inbounds = %d", c)
	}
	mustStatus(t, e.call(t, http.MethodPost, fmt.Sprintf("/v1/admin/nodes/%d/inbounds", id), realityBody("main", "www.microsoft.com")), http.StatusOK)
	if c := count(); c != 1 {
		t.Fatalf("inbounds = %d", c)
	}
}
