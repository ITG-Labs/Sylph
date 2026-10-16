package api

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxDeltaBytes = 1 << 50

const maxConfigErrorRunes = 512

func truncateRunes(s *string, n int) *string {
	if s == nil {
		return nil
	}
	r := []rune(*s)
	if len(r) <= n {
		return s
	}
	out := string(r[:n])
	return &out
}

func (s *Server) registerSyncRoutes(mux *http.ServeMux) {
	s.route(mux, "POST /v1/sync/users", s.handleSyncUsers, mtls.RoleNode)
	s.route(mux, "POST /v1/sync/deltas", s.handleSyncDeltas, mtls.RoleNode)
	s.route(mux, "POST /v1/sync/heartbeat", s.handleSyncHeartbeat, mtls.RoleNode)
}

func clientCN(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	return r.TLS.PeerCertificates[0].Subject.CommonName
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

func (s *Server) handleSyncUsers(w http.ResponseWriter, r *http.Request) {
	cn := clientCN(r)
	if cn == "" {
		writeError(w, http.StatusForbidden, "forbidden", "client certificate required")
		return
	}
	var body struct {
		KnownVersion int64 `json:"known_version"`
	}
	if !decodeJSON(w, r, &body, maxBodyBytes, false) {
		return
	}
	resp, err := s.svc.SyncNodeUsers(r.Context(), cn, body.KnownVersion)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusForbidden, "unknown_node", "node not registered")
			return
		}
		s.log.Error("sync_users_failed", "error", err.Error(), "node", cn)
		writeError(w, http.StatusInternalServerError, "internal", "sync failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSyncDeltas(w http.ResponseWriter, r *http.Request) {
	cn := clientCN(r)
	if cn == "" {
		writeError(w, http.StatusForbidden, "forbidden", "client certificate required")
		return
	}
	var body struct {
		BatchID string `json:"batch_id"`
		Deltas  []struct {
			KeyUUID   string `json:"key_uuid"`
			BytesUp   int64  `json:"bytes_up"`
			BytesDown int64  `json:"bytes_down"`
		} `json:"deltas"`
	}
	if !decodeJSON(w, r, &body, maxDeltasBytes, false) {
		return
	}
	if body.BatchID == "" || len(body.BatchID) > 64 {
		writeError(w, http.StatusBadRequest, "bad_request", "batch_id must be 1-64 characters")
		return
	}
	deltas := make([]service.TrafficDeltaInput, 0, len(body.Deltas))
	for _, d := range body.Deltas {
		if d.BytesUp < 0 || d.BytesDown < 0 || d.BytesUp > maxDeltaBytes || d.BytesDown > maxDeltaBytes {
			writeError(w, http.StatusBadRequest, "bad_request", "traffic bytes must be between 0 and 1 PiB")
			return
		}
		deltas = append(deltas, service.TrafficDeltaInput{KeyUUID: d.KeyUUID, BytesUp: d.BytesUp, BytesDown: d.BytesDown})
	}
	duplicate, err := s.svc.RecordNodeDeltasInput(r.Context(), cn, body.BatchID, deltas)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusForbidden, "unknown_node", "node not registered")
			return
		}
		s.log.Error("sync_deltas_failed", "error", err.Error(), "node", cn)
		writeError(w, http.StatusInternalServerError, "internal", "deltas failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true, "duplicate": duplicate})
}

func (s *Server) handleSyncHeartbeat(w http.ResponseWriter, r *http.Request) {
	cn := clientCN(r)
	if cn == "" {
		writeError(w, http.StatusForbidden, "forbidden", "client certificate required")
		return
	}
	var body struct {
		AgentVersion   string          `json:"agent_version"`
		XrayVersion    string          `json:"xray_version"`
		AgentUptimeSec int64           `json:"agent_uptime_sec"`
		XrayUptimeSec  *int64          `json:"xray_uptime_sec"`
		ActiveUsers    *int            `json:"active_users"`
		ConfigError    *string         `json:"config_error"`
		GeoStatus      json.RawMessage `json:"geo_status"`
		RoutingError   *string         `json:"routing_error"`
	}
	if !decodeJSON(w, r, &body, maxBodyBytes, false) {
		return
	}
	if body.AgentUptimeSec < 0 ||
		(body.XrayUptimeSec != nil && *body.XrayUptimeSec < 0) ||
		(body.ActiveUsers != nil && (*body.ActiveUsers < 0 || *body.ActiveUsers > math.MaxInt32)) {
		writeError(w, http.StatusBadRequest, "bad_request", "uptime and active_users must be non-negative")
		return
	}
	hb := model.NodeHeartbeat{
		AgentVersion:   body.AgentVersion,
		XrayVersion:    body.XrayVersion,
		AgentUptimeSec: body.AgentUptimeSec,
		XrayUptimeSec:  body.XrayUptimeSec,
		ActiveUsers:    body.ActiveUsers,
		ConfigError:    truncateRunes(body.ConfigError, maxConfigErrorRunes),
		GeoStatus:      geoStatusOf(body.GeoStatus),
		RoutingError:   truncateRunes(body.RoutingError, maxConfigErrorRunes),
	}
	if err := s.svc.NodeHeartbeat(r.Context(), cn, hb); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusForbidden, "unknown_node", "node not registered")
			return
		}
		s.log.Error("heartbeat_failed", "error", err.Error(), "node", cn)
		writeError(w, http.StatusInternalServerError, "internal", "heartbeat failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
