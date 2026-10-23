package api

import (
	"net/http"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
)

func (s *Server) registerNotificationRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /v1/notifications/due", s.handleNotificationsDue, mtls.RoleBot)
	s.route(mux, "POST /v1/notifications/sent", s.handleNotificationSent, mtls.RoleBot)
}

func (s *Server) handleNotificationsDue(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.DueNotifications(r.Context(), time.Now().UTC())
	if err != nil {
		s.writeServiceErrorAs(w, err, "notifications", "notifications_failed")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) handleNotificationSent(w http.ResponseWriter, r *http.Request) {
	var in service.NotificationSent
	if !decodeBody(w, r, &in) {
		return
	}
	if err := s.svc.MarkNotified(r.Context(), in); err != nil {
		s.writeServiceErrorAs(w, err, "user", "notifications_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
