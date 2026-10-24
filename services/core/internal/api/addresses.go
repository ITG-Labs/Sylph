package api

import (
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func (s *Server) registerAddressRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /v1/admin/addresses", s.handleAddressList, mtls.RolePanel)
	s.route(mux, "PUT /v1/admin/addresses/{id}/assignment", s.handleAddressAssign, mtls.RolePanel)
	s.route(mux, "DELETE /v1/admin/addresses/{id}/assignment", s.handleAddressUnassign, mtls.RolePanel)
	s.route(mux, "DELETE /v1/admin/addresses/{id}", s.handleAddressDelete, mtls.RolePanel)
}

func (s *Server) handleAddressList(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.ListAddresses(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "addresses")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": list})
}

func (s *Server) handleAddressAssign(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		UserID int64  `json:"user_id"`
		Note   string `json:"note"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	a, err := s.svc.AssignAddress(r.Context(), id, body.UserID, body.Note)
	if err != nil {
		s.writeServiceError(w, err, "address")
		return
	}
	s.logChange(r, "address", "assign", id)
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleAddressUnassign(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.svc.UnassignAddress(r.Context(), id); err != nil {
		s.writeServiceError(w, err, "assignment")
		return
	}
	s.logChange(r, "address", "unassign", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAddressDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.svc.DeleteAddress(r.Context(), id); err != nil {
		s.writeServiceError(w, err, "address")
		return
	}
	s.logChange(r, "address", "delete", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
