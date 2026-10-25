package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxDeltaBytes = 1 << 50

const maxConfigErrorRunes = 512

const (
	maxHeartbeatBytes  = 4 << 20
	maxOnlineReports   = 20000
	maxOnlineIPsPerKey = 32
	maxOnlineAgeSec    = 30 * 24 * 3600
)

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
	b, err := json.Marshal(v)
	if err != nil {
		logging.RecordError(w, fmt.Errorf("encode response: %w", err))
		status = http.StatusInternalServerError
		b = []byte(`{"code":"internal","message":"encode failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(append(b, '\n')); err != nil {
		logging.RecordError(w, fmt.Errorf("write response: %w", err))
	}
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
		Addresses      *[]struct {
			IP    string `json:"ip"`
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"addresses"`
		Online *[]struct {
			Key string `json:"key"`
			IPs []struct {
				IP     string `json:"ip"`
				AgeSec int64  `json:"age_sec"`
			} `json:"ips"`
		} `json:"online"`
	}
	if !decodeJSON(w, r, &body, maxHeartbeatBytes, false) {
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
	if body.Addresses != nil {
		list := *body.Addresses
		if len(list) > store.MaxNodeAddresses {
			s.log.Warn("heartbeat_addresses_truncated", "node", cn, "count", len(list))
			list = list[:store.MaxNodeAddresses]
		}
		reports := make([]model.AddressReport, 0, len(list))
		seen := map[string]bool{}
		for _, a := range list {
			ip, err := netip.ParseAddr(a.IP)
			if err != nil || !ip.Is4() || ip.String() != a.IP || seen[a.IP] {
				writeError(w, http.StatusBadRequest, "bad_request", "addresses must be distinct IPv4 addresses")
				return
			}
			seen[a.IP] = true
			msg := ""
			if !a.OK {
				msg = *truncateRunes(&a.Error, maxConfigErrorRunes)
			}
			reports = append(reports, model.AddressReport{IP: a.IP, OK: a.OK, Error: msg})
		}
		hb.Addresses = &reports
	}
	if body.Online != nil {
		now := time.Now()
		reports := []model.OnlineReport{}
		skipped := 0
		for _, o := range *body.Online {
			if len(reports) >= maxOnlineReports {
				s.log.Warn("heartbeat_online_truncated", "node", cn, "keys", len(*body.Online))
				break
			}
			if o.Key == "" || len(o.Key) > 64 {
				skipped += len(o.IPs)
				continue
			}
			for i, ip := range o.IPs {
				if i >= maxOnlineIPsPerKey {
					break
				}
				addr, ok := onlineAddr(ip.IP)
				if !ok {
					skipped++
					continue
				}
				age := min(max(ip.AgeSec, 0), maxOnlineAgeSec)
				reports = append(reports, model.OnlineReport{Key: o.Key, IP: addr, Since: now.Add(-time.Duration(age) * time.Second)})
			}
		}
		if skipped > 0 {
			s.log.Warn("heartbeat_online_skipped", "node", cn, "entries", skipped)
		}
		hb.Online = &reports
	}
	if cert := peerCert(r); cert != nil {
		hb.CertNotAfter = &cert.NotAfter
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

func onlineAddr(raw string) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
	if err != nil {
		return "", false
	}
	return addr.WithZone("").Unmap().String(), true
}
