package api

import (
	"net/http"

	"github.com/ITG-Labs/sylph/services/core/internal/service"
)

const maxHWIDRunes = 256

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SubToken    string `json:"sub_token"`
		HWID        string `json:"hwid"`
		UserAgent   string `json:"user_agent"`
		DeviceOS    string `json:"device_os"`
		DeviceModel string `json:"device_model"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.SubToken == "" {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "sub_token", "is required")
		return
	}
	if len([]rune(body.HWID)) > maxHWIDRunes {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "hwid", "must be at most 256 characters")
		return
	}
	view, err := s.svc.Subscription(r.Context(), service.SubscriptionRequest{
		SubToken:    body.SubToken,
		HWID:        body.HWID,
		UserAgent:   clip(body.UserAgent, 256),
		DeviceOS:    clip(body.DeviceOS, 64),
		DeviceModel: clip(body.DeviceModel, 64),
	})
	if err != nil {
		s.writeServiceErrorAs(w, err, "subscription", "subscription_failed")
		return
	}
	writeJSON(w, http.StatusOK, view)
}
