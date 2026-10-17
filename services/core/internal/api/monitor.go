package api

import (
	"net/http"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func (s *Server) registerMonitorRoutes(mux *http.ServeMux) {
	s.routeMachine(mux, "POST /v1/services/heartbeat", s.handleServiceHeartbeat,
		mtls.RoleBot, mtls.RoleSubs, mtls.RoleCron, mtls.RolePanel, mtls.RoleMonitor)
	s.route(mux, "GET /v1/monitor/state", s.handleMonitorState, mtls.RoleMonitor)
	s.route(mux, "DELETE /v1/admin/services/{cn}", s.handleServiceForget, mtls.RolePanel)
}

func (s *Server) handleServiceHeartbeat(w http.ResponseWriter, r *http.Request) {
	cert := peerCert(r)
	role, ok := mtls.RoleOf(cert)
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "role not allowed")
		return
	}
	var body struct {
		Version            string     `json:"version"`
		UptimeSec          int64      `json:"uptime_sec"`
		TelegramConflictAt *time.Time `json:"telegram_conflict_at"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	hb := model.ServiceHeartbeat{
		CertCN:             cert.Subject.CommonName,
		Role:               role,
		Version:            body.Version,
		UptimeSec:          body.UptimeSec,
		CertNotAfter:       &cert.NotAfter,
		TelegramConflictAt: body.TelegramConflictAt,
	}
	if err := s.svc.ServiceHeartbeat(r.Context(), hb); err != nil {
		s.writeServiceError(w, err, "service heartbeat")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMonitorState(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.MonitorState(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "monitor state")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleServiceForget(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.ForgetService(r.Context(), r.PathValue("cn")); err != nil {
		s.writeServiceError(w, err, "service")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
