package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/logging"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
)

const maxGeoStatusBytes = 16 << 10

func (s *Server) registerRoutingRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /v1/admin/profiles", s.handleProfileList, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/profiles", s.handleProfileCreate, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/profiles/{id}", s.handleProfileGet, mtls.RolePanel)
	s.route(mux, "PUT /v1/admin/profiles/{id}", s.handleProfileUpdate, mtls.RolePanel)
	s.route(mux, "DELETE /v1/admin/profiles/{id}", s.handleProfileDelete, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/profiles/{id}/preview", s.handleProfilePreview, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/geo-sources", s.handleGeoList, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/geo-sources", s.handleGeoCreate, mtls.RolePanel)
	s.route(mux, "PATCH /v1/admin/geo-sources/{id}", s.handleGeoUpdate, mtls.RolePanel)
	s.route(mux, "DELETE /v1/admin/geo-sources/{id}", s.handleGeoDelete, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/geo-sources/refresh", s.handleGeoRefresh, mtls.RolePanel)
}

func (s *Server) handleProfileList(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.ListProfiles(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "profiles")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string          `json:"name"`
		Doc  json.RawMessage `json:"doc"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	v, err := s.svc.CreateProfile(r.Context(), body.Name, body.Doc)
	if err != nil {
		s.writeServiceError(w, err, "profile")
		return
	}
	s.logChange(r, "profile", "create", v.ID)
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleProfileGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v, err := s.svc.GetProfile(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err, "profile")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleProfileUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Name     string          `json:"name"`
		Doc      json.RawMessage `json:"doc"`
		Revision int64           `json:"revision"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	v, err := s.svc.UpdateProfile(r.Context(), id, body.Name, body.Doc, body.Revision)
	if err != nil {
		s.writeServiceError(w, err, "profile")
		return
	}
	s.logChange(r, "profile", "update", id)
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleProfileDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.svc.DeleteProfile(r.Context(), id); err != nil {
		s.writeServiceError(w, err, "profile")
		return
	}
	s.logChange(r, "profile", "delete", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleProfilePreview(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	q, ok := readQuery(w, r, "node_id")
	if !ok {
		return
	}
	nodeID := q.optID("node_id")
	if !q.ok() {
		return
	}
	raw, err := s.svc.PreviewProfile(r.Context(), id, nodeID)
	if err != nil {
		s.writeServiceError(w, err, "profile")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(raw); err != nil {
		logging.RecordError(w, fmt.Errorf("write response: %w", err))
	}
}

func (s *Server) handleGeoList(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.ListGeoSources(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "geo sources")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGeoCreate(w http.ResponseWriter, r *http.Request) {
	var body service.GeoSourceInput
	if !decodeBody(w, r, &body) {
		return
	}
	g, err := s.svc.CreateGeoSource(r.Context(), body)
	if err != nil {
		s.writeServiceError(w, err, "geo source")
		return
	}
	s.logChange(r, "geo_source", "create", g.ID)
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleGeoUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var patch struct {
		Name      *string `json:"name"`
		Kind      *string `json:"kind"`
		URL       *string `json:"url"`
		SHA256URL *string `json:"sha256_url"`
	}
	if !decodeBody(w, r, &patch) {
		return
	}
	current, err := s.svc.GetGeoSource(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err, "geo source")
		return
	}
	in := service.GeoSourceInput{Name: current.Name, Kind: current.Kind, URL: current.URL, SHA256URL: current.SHA256URL}
	for _, f := range []struct {
		src *string
		dst *string
	}{{patch.Name, &in.Name}, {patch.Kind, &in.Kind}, {patch.URL, &in.URL}, {patch.SHA256URL, &in.SHA256URL}} {
		if f.src != nil {
			*f.dst = *f.src
		}
	}
	g, err := s.svc.UpdateGeoSource(r.Context(), id, in)
	if err != nil {
		s.writeServiceError(w, err, "geo source")
		return
	}
	s.logChange(r, "geo_source", "update", id)
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleGeoDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.svc.DeleteGeoSource(r.Context(), id); err != nil {
		s.writeServiceError(w, err, "geo source")
		return
	}
	s.logChange(r, "geo_source", "delete", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleGeoRefresh(w http.ResponseWriter, r *http.Request) {
	gen, err := s.svc.RequestGeoRefresh(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "geo sources")
		return
	}
	s.logChange(r, "geo_source", "refresh", "")
	writeJSON(w, http.StatusOK, map[string]int64{"generation": gen})
}

func geoStatusOf(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || len(raw) > maxGeoStatusBytes {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil
	}
	return raw
}
