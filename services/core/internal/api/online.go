package api

import (
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

const maxIPHistoryLimit = 100

func (s *Server) registerOnlineRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /v1/admin/users/{id}/ips", s.handleUserIPs, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/nodes/{id}/online", s.handleNodeOnline, mtls.RolePanel)
}

func (s *Server) handleUserIPs(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	q, ok := readQuery(w, r, "limit", "offset")
	if !ok {
		return
	}
	limit, offset := int(q.int("limit", 1, maxIPHistoryLimit, 50)), int(q.int("offset", 0, 1<<31-1, 0))
	if !q.ok() {
		return
	}
	rows, total, err := s.svc.UserIPHistory(r.Context(), id, limit, offset)
	if err != nil {
		s.writeServiceError(w, err, "user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows, "total": total})
}

func (s *Server) handleNodeOnline(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.svc.NodeOnline(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err, "node")
		return
	}
	writeJSON(w, http.StatusOK, list)
}
