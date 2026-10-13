package api

import (
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Server) registerUserAdminRoutes(mux *http.ServeMux) {
	s.route(mux, "POST /v1/admin/users/{id}/ban", s.handleUserBan, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/users/{id}/unban", s.handleUserUnban, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/users/{id}/extend", s.handleUserExtend, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/users/{id}/traffic", s.handleUserTraffic, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/users/{id}/plan", s.handleUserPlan, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/periods/{id}/close", s.handlePeriodClose, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/users", s.handleUserList, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/users/{id}", s.handleUserCard, mtls.RolePanel)
}

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	q, ok := readQuery(w, r, "q", "status", "subscription", "plan_id", "before", "limit")
	if !ok {
		return
	}
	query := service.UserQuery{
		Q: q.str("q"), Status: q.str("status"), Subscription: q.str("subscription"),
		PlanID: q.optID("plan_id"), Before: q.int("before", 0, 1<<62, 0), Limit: int(q.int("limit", 1, 100, 50)),
	}
	if !q.ok() {
		return
	}
	rows, err := s.svc.ListUsers(r.Context(), query)
	if err != nil {
		s.writeServiceError(w, err, "users")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": rows, "next_before": nextBefore(rows, query.Limit, func(u store.UserRow) int64 { return u.ID })})
}

func (s *Server) handleUserCard(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	card, err := s.svc.UserCard(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err, "user")
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) handleUserBan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	already, err := s.svc.BanUser(r.Context(), id, body.Reason)
	if err != nil {
		s.writeServiceError(w, err, "user")
		return
	}
	s.logChange(r, "user", "ban", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "already": already})
}

func (s *Server) handleUserUnban(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	already, err := s.svc.UnbanUser(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err, "user")
		return
	}
	s.logChange(r, "user", "unban", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "already": already})
}

func (s *Server) handleUserExtend(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Days int `json:"days"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	sub, err := s.svc.ExtendSubscription(r.Context(), id, body.Days)
	if err != nil {
		s.writeServiceError(w, err, "user")
		return
	}
	s.logChange(r, "subscription", "extend", sub.ID)
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleUserTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Bytes int64 `json:"bytes"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	sub, err := s.svc.AddTraffic(r.Context(), id, body.Bytes)
	if err != nil {
		s.writeServiceError(w, err, "user")
		return
	}
	s.logChange(r, "subscription", "traffic", sub.ID)
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleUserPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		PlanID int64 `json:"plan_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	changed, err := s.svc.ChangeSubscriptionPlan(r.Context(), id, body.PlanID)
	if err != nil {
		s.writeServiceError(w, err, "user or plan")
		return
	}
	s.logChange(r, "subscription", "plan", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "changed": changed})
}

func (s *Server) handlePeriodClose(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.svc.ClosePeriod(r.Context(), id); err != nil {
		s.writeServiceError(w, err, "period")
		return
	}
	s.logChange(r, "subscription", "close", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
