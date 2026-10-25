package api

import (
	"net/http"
	"strconv"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func (s *Server) registerBotRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /v1/bot/config", s.handleBotConfig, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/users/{user_id}/terms", s.handleTermsAccept, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/settings/terms/bump", s.handleTermsBump, mtls.RolePanel)
}

func (s *Server) handleBotConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.svc.BotConfig(r.Context())
	if err != nil {
		s.writeServiceErrorAs(w, err, "bot config", "bot_config_failed")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleTermsAccept(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id")
		return
	}
	var body struct {
		Version int `json:"version"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Version < 1 {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "version", "must be a positive integer")
		return
	}
	if err := s.svc.AcceptTerms(r.Context(), userID, body.Version); err != nil {
		s.writeServiceErrorAs(w, err, "user", "terms_accept_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"version": body.Version})
}

func (s *Server) handleTermsBump(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.BumpTerms(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "settings")
		return
	}
	s.logChange(r, "settings", "terms_bump", strconv.Itoa(st.TermsVersion))
	writeJSON(w, http.StatusOK, st)
}
