package api

import (
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/clientroute"
	"github.com/ITG-Labs/sylph/pkg/mtls"
)

const maxGeoTagsBody = 2 << 20

func (s *Server) registerClientRoutingRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /v1/admin/client-routing", s.handleClientRoutingGet, mtls.RolePanel)
	s.route(mux, "PUT /v1/admin/client-routing", s.handleClientRoutingPut, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/client-routing/geo-tags", s.handleClientGeoTagsGet, mtls.RolePanel)
	s.route(mux, "POST /v1/maintenance/geo-tags", s.handleClientGeoTagsPut, mtls.RoleCron)
}

func (s *Server) handleClientRoutingGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.GetClientRouting(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "client_routing")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleClientRoutingPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rules    []clientroute.Rule `json:"rules"`
		Revision int64              `json:"revision"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	v, err := s.svc.PutClientRouting(r.Context(), body.Rules, body.Revision)
	if err != nil {
		s.writeServiceError(w, err, "client_routing")
		return
	}
	s.logChange(r, "client_routing", "update", "")
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleClientGeoTagsGet(w http.ResponseWriter, r *http.Request) {
	tags, err := s.svc.ClientGeoTags(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "client_routing")
		return
	}
	if tags == nil {
		tags = &clientroute.Tags{Geosite: []string{}, Geoip: []string{}}
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleClientGeoTagsPut(w http.ResponseWriter, r *http.Request) {
	var body clientroute.Tags
	if !decodeJSON(w, r, &body, maxGeoTagsBody, true) {
		return
	}
	changed, err := s.svc.PutClientGeoTags(r.Context(), body)
	if err != nil {
		s.writeServiceError(w, err, "client_routing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"changed": changed})
}
