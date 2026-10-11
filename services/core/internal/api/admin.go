package api

import (
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
)

func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	s.route(mux, "POST /v1/admin/groups", s.handleGroupCreate, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/groups", s.handleGroupList, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/plans", s.handlePlanCreate, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/plans", s.handlePlanList, mtls.RolePanel)
	s.route(mux, "PATCH /v1/admin/plans/{id}", s.handlePlanUpdate, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/nodes", s.handleNodeCreate, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/nodes", s.handleNodeList, mtls.RolePanel)
	s.route(mux, "PATCH /v1/admin/nodes/{id}", s.handleNodeUpdate, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/nodes/{id}/inbounds", s.handleInboundList, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/nodes/{id}/inbounds", s.handleInboundCreate, mtls.RolePanel)
	s.route(mux, "PATCH /v1/admin/inbounds/{id}", s.handleInboundUpdate, mtls.RolePanel)
	s.route(mux, "DELETE /v1/admin/inbounds/{id}", s.handleInboundDelete, mtls.RolePanel)
}

func (s *Server) logChange(r *http.Request, entity, action string, id int64) {
	s.log.Info("admin_change", "entity", entity, "action", action, "id", id, "peer", clientCN(r))
}

func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	g, err := s.svc.CreateGroup(r.Context(), body.Name)
	if err != nil {
		s.writeServiceError(w, err, "group")
		return
	}
	s.logChange(r, "group", "create", g.ID)
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleGroupList(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.ListGroups(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "groups")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) handlePlanCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name              string `json:"name"`
		PriceRub          int64  `json:"price_rub"`
		PeriodDays        int    `json:"period_days"`
		TrafficLimitBytes *int64 `json:"traffic_limit_bytes"`
		LocationGroupID   *int64 `json:"location_group_id"`
		DeviceLimit       *int   `json:"device_limit"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	p := &model.Plan{
		Name: body.Name, PriceRub: body.PriceRub, PeriodDays: body.PeriodDays,
		TrafficLimitBytes: body.TrafficLimitBytes, LocationGroupID: body.LocationGroupID, DeviceLimit: body.DeviceLimit,
	}
	if err := s.svc.CreatePlanChecked(r.Context(), p); err != nil {
		s.writeServiceError(w, err, "plan")
		return
	}
	s.logChange(r, "plan", "create", p.ID)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handlePlanList(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.ListPlans(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "plans")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) handlePlanUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var patch service.PlanPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	p, err := s.svc.UpdatePlan(r.Context(), id, patch)
	if err != nil {
		s.writeServiceError(w, err, "plan")
		return
	}
	s.logChange(r, "plan", "update", id)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleNodeCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CertCN  string `json:"cert_cn"`
		Name    string `json:"name"`
		Address string `json:"address"`
		GroupID *int64 `json:"group_id"`
		Enabled *bool  `json:"enabled"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	n := &model.Node{CertCN: body.CertCN, Name: body.Name, Address: body.Address, GroupID: body.GroupID, Enabled: true}
	if body.Enabled != nil {
		n.Enabled = *body.Enabled
	}
	if err := s.svc.CreateNode(r.Context(), n); err != nil {
		s.writeServiceError(w, err, "node")
		return
	}
	created, err := s.svc.GetNode(r.Context(), n.ID)
	if err != nil {
		s.writeServiceError(w, err, "node")
		return
	}
	s.logChange(r, "node", "create", n.ID)
	writeJSON(w, http.StatusOK, created)
}

func (s *Server) handleNodeList(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.ListNodes(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "nodes")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) handleNodeUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var patch service.NodePatch
	if !decodeBody(w, r, &patch) {
		return
	}
	n, err := s.svc.UpdateNode(r.Context(), id, patch)
	if err != nil {
		s.writeServiceError(w, err, "node")
		return
	}
	s.logChange(r, "node", "update", id)
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleInboundList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.svc.ListInbounds(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err, "node")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleInboundCreate(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Tag       string           `json:"tag"`
		Port      int              `json:"port"`
		Protocol  string           `json:"protocol"`
		Transport string           `json:"transport"`
		Security  string           `json:"security"`
		Label     string           `json:"label"`
		Reality   *inbound.Reality `json:"reality"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	spec := inbound.Spec{
		Tag: body.Tag, Port: body.Port, Protocol: body.Protocol, Transport: body.Transport,
		Security: body.Security, Label: body.Label, Reality: body.Reality,
	}
	v, err := s.svc.CreateInbound(r.Context(), nodeID, spec)
	if err != nil {
		s.writeServiceError(w, err, "node")
		return
	}
	s.logChange(r, "inbound", "create", v.ID)
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleInboundUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var patch service.InboundPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	v, err := s.svc.UpdateInbound(r.Context(), id, patch)
	if err != nil {
		s.writeServiceError(w, err, "inbound")
		return
	}
	s.logChange(r, "inbound", "update", id)
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleInboundDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.svc.DeleteInbound(r.Context(), id); err != nil {
		s.writeServiceError(w, err, "inbound")
		return
	}
	s.logChange(r, "inbound", "delete", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
