package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Server) registerPrivateRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /v1/users/{user_id}/plans", s.handleUserPlans, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/invites/redeem", s.handleInviteRedeem, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/plans/{id}/invites", s.handleInviteCreate, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/plans/{id}/invites", s.handleInviteList, mtls.RolePanel)
	s.route(mux, "DELETE /v1/admin/invites/{id}", s.handleInviteRevoke, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/plans/{id}/access", s.handleAccessList, mtls.RolePanel)
	s.route(mux, "PUT /v1/admin/plans/{id}/access/{telegram_id}", s.handleAccessGrant, mtls.RolePanel)
	s.route(mux, "DELETE /v1/admin/plans/{id}/access/{telegram_id}", s.handleAccessRevoke, mtls.RolePanel)
}

func (s *Server) handleUserPlans(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id")
		return
	}
	list, err := s.svc.UserPlans(r.Context(), userID)
	if err != nil {
		s.writeServiceError(w, err, "user")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleInviteRedeem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID int64  `json:"user_id"`
		Code   string `json:"code"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.UserID <= 0 {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "user_id", "must be positive")
		return
	}
	plan, already, err := s.svc.RedeemInvite(r.Context(), body.UserID, body.Code)
	switch {
	case errors.Is(err, store.ErrInviteRevoked):
		writeError(w, http.StatusConflict, "invite_revoked", "invite is revoked")
	case errors.Is(err, store.ErrInviteExpired):
		writeError(w, http.StatusConflict, "invite_expired", "invite is expired")
	case errors.Is(err, store.ErrInviteUsedUp):
		writeError(w, http.StatusConflict, "invite_used_up", "invite is used up")
	case errors.Is(err, store.ErrAccessRevoked):
		writeError(w, http.StatusConflict, "access_revoked", "access to this plan was revoked")
	case err != nil:
		s.writeServiceError(w, err, "user or invite")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"plan": plan, "already": already})
	}
}

func (s *Server) handleInviteCreate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		MaxUses   *int       `json:"max_uses"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	inv, err := s.svc.CreateInvite(r.Context(), id, body.MaxUses, body.ExpiresAt)
	if err != nil {
		s.writeServiceError(w, err, "plan")
		return
	}
	s.logChange(r, "invite", "create", inv.ID)
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) handleInviteList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.svc.ListInvites(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err, "plan")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleInviteRevoke(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	inv, err := s.svc.RevokeInvite(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err, "invite")
		return
	}
	s.logChange(r, "invite", "revoke", id)
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) handleAccessList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.svc.ListPlanAccess(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err, "plan")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func accessPath(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return 0, 0, false
	}
	tg, err := strconv.ParseInt(r.PathValue("telegram_id"), 10, 64)
	if err != nil || tg <= 0 {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "telegram_id", "must be a positive integer")
		return 0, 0, false
	}
	return id, tg, true
}

func (s *Server) handleAccessGrant(w http.ResponseWriter, r *http.Request) {
	id, tg, ok := accessPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.GrantPlanAccess(r.Context(), id, tg); err != nil {
		s.writeServiceError(w, err, "plan")
		return
	}
	s.logChange(r, "plan_access", "grant", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAccessRevoke(w http.ResponseWriter, r *http.Request) {
	id, tg, ok := accessPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RevokePlanAccess(r.Context(), id, tg); err != nil {
		s.writeServiceError(w, err, "access")
		return
	}
	s.logChange(r, "plan_access", "revoke", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
