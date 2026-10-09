package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Server) registerSyncRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/sync/users", s.handleSyncUsers)
	mux.HandleFunc("POST /v1/sync/deltas", s.handleSyncDeltas)
	mux.HandleFunc("POST /v1/sync/heartbeat", s.handleSyncHeartbeat)
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
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
		Deltas []struct {
			KeyUUID    string `json:"key_uuid"`
			BytesUp    int64  `json:"bytes_up"`
			BytesDown  int64  `json:"bytes_down"`
		} `json:"deltas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	deltas := make([]service.TrafficDeltaInput, 0, len(body.Deltas))
	for _, d := range body.Deltas {
		deltas = append(deltas, service.TrafficDeltaInput{KeyUUID: d.KeyUUID, BytesUp: d.BytesUp, BytesDown: d.BytesDown})
	}
	if err := s.svc.RecordNodeDeltasInput(r.Context(), cn, deltas); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusForbidden, "unknown_node", "node not registered")
			return
		}
		s.log.Error("sync_deltas_failed", "error", err.Error(), "node", cn)
		writeError(w, http.StatusInternalServerError, "internal", "deltas failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Server) handleSyncHeartbeat(w http.ResponseWriter, r *http.Request) {
	cn := clientCN(r)
	if cn == "" {
		writeError(w, http.StatusForbidden, "forbidden", "client certificate required")
		return
	}
	var body struct {
		AgentVersion string `json:"agent_version"`
		XrayVersion  string `json:"xray_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.svc.NodeHeartbeat(r.Context(), cn, body.AgentVersion, body.XrayVersion); err != nil {
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
