package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func TestBotGatesFlow(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	bot := e.client(t, "bot", mtls.RoleBot)

	var reg struct {
		ID           int64 `json:"id"`
		TermsVersion *int  `json:"terms_version"`
	}
	r := adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users", map[string]any{"telegram_id": 8401})
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &reg)
	if reg.TermsVersion == nil || *reg.TermsVersion != 0 {
		t.Fatalf("fresh user register = %s", r.body)
	}

	patch := map[string]any{
		"terms_enabled": true, "terms_url": "https://telegra.ph/terms", "terms_text": "Прочитай <b>это</b>",
		"channel_enabled": true, "channel_chat": "@nora_vpn",
		"help_enabled": true, "help_text": "Помощь", "help_buttons": []map[string]string{{"label": "Поддержка", "url": "https://t.me/nora_help"}},
	}
	r = adminCall(t, e.panel, http.MethodPatch, e.ts.URL+"/v1/admin/settings", patch)
	mustStatus(t, r, http.StatusOK)
	mustStatus(t, adminCall(t, e.panel, http.MethodPatch, e.ts.URL+"/v1/admin/settings", map[string]any{"help_text": "<script>"}), http.StatusBadRequest)

	var cfg struct {
		Terms struct {
			Enabled bool
			Text    string
			URL     string
			Version int
		}
		Channel struct {
			Enabled bool
			Chat    string
			URL     string
		}
		Help struct {
			Enabled bool
			Text    string
			Buttons []struct{ Label, URL string }
		}
	}
	r = adminCall(t, bot, http.MethodGet, e.ts.URL+"/v1/bot/config", nil)
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &cfg)
	if !cfg.Terms.Enabled || cfg.Terms.Version != 1 || cfg.Terms.Text != "Прочитай <b>это</b>" || cfg.Channel.URL != "https://t.me/nora_vpn" ||
		!cfg.Help.Enabled || len(cfg.Help.Buttons) != 1 || cfg.Help.Buttons[0].Label != "Поддержка" {
		t.Fatalf("config = %s", r.body)
	}

	terms := fmt.Sprintf("%s/v1/users/%d/terms", e.ts.URL, reg.ID)
	mustStatus(t, adminCall(t, bot, http.MethodPost, terms, map[string]int{"version": 2}), http.StatusConflict)
	mustStatus(t, adminCall(t, bot, http.MethodPost, terms, map[string]int{"version": 0}), http.StatusBadRequest)
	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users/999999/terms", map[string]int{"version": 1}), http.StatusNotFound)
	mustStatus(t, adminCall(t, bot, http.MethodPost, terms, map[string]int{"version": 1}), http.StatusOK)
	r = adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/users", map[string]any{"telegram_id": 8401})
	r.decode(t, &reg)
	if *reg.TermsVersion != 1 {
		t.Fatalf("after accept register = %s", r.body)
	}

	mustStatus(t, adminCall(t, bot, http.MethodPost, e.ts.URL+"/v1/admin/settings/terms/bump", nil), http.StatusForbidden)
	r = operatorCall(t, e.panel, http.MethodPost, e.ts.URL+"/v1/admin/settings/terms/bump", "Иван", "")
	mustStatus(t, r, http.StatusOK)
	var st struct {
		TermsVersion int `json:"terms_version"`
	}
	r.decode(t, &st)
	if st.TermsVersion != 2 {
		t.Fatalf("bump = %s", r.body)
	}
	var action, entityID string
	if err := e.pool.QueryRow(ctx, `SELECT action, entity_id FROM admin_audit WHERE entity = 'settings' ORDER BY id DESC LIMIT 1`).Scan(&action, &entityID); err != nil || action != "terms_bump" || entityID != "2" {
		t.Fatalf("audit = %s %s, %v", action, entityID, err)
	}
	mustStatus(t, adminCall(t, bot, http.MethodPost, terms, map[string]int{"version": 1}), http.StatusConflict)

	var card struct {
		Terms *struct {
			Version int `json:"version"`
		} `json:"terms"`
	}
	r = adminCall(t, e.panel, http.MethodGet, fmt.Sprintf("%s/v1/admin/users/%d", e.ts.URL, reg.ID), nil)
	mustStatus(t, r, http.StatusOK)
	r.decode(t, &card)
	if card.Terms == nil || card.Terms.Version != 1 {
		t.Fatalf("card terms = %s", r.body)
	}
}
