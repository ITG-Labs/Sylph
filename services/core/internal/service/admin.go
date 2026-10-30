package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

type FieldError = inbound.FieldError

type ConflictError struct {
	Field   string
	Message string
}

func (e *ConflictError) Error() string {
	return e.Field + ": " + e.Message
}

var certCNPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func fieldErr(field, message string) error {
	return &FieldError{Field: field, Message: message}
}

func conflictOr(err error, field, message string) error {
	if errors.Is(err, store.ErrConflict) {
		return &ConflictError{Field: field, Message: message}
	}
	return err
}

func referenceOr(err error, field, message string) error {
	if errors.Is(err, store.ErrInvalidReference) {
		return fieldErr(field, message)
	}
	return err
}

func validText(field, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return fieldErr(field, fmt.Sprintf("must be at most %d characters", max))
	}
	for _, r := range v {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fieldErr(field, "must not contain control characters")
		}
	}
	return nil
}

const (
	maxGroupMembers = 1000
	maxPlanPools    = 32
)

func nonNilIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

func validName(field, name string) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 64 {
		return fieldErr(field, "must be 1-64 characters, not only spaces")
	}
	return validText(field, name, 64)
}

func (s *Service) CreateGroup(ctx context.Context, name string) (*model.LocationGroup, error) {
	if err := validName("name", name); err != nil {
		return nil, err
	}
	g, err := s.store.CreateLocationGroup(ctx, name)
	return g, conflictOr(err, "name", "group already exists")
}

func (s *Service) ListGroups(ctx context.Context) ([]model.LocationGroup, error) {
	return s.store.ListLocationGroups(ctx)
}

func (s *Service) RenameGroup(ctx context.Context, id int64, name string) (*model.LocationGroup, error) {
	if err := validName("name", name); err != nil {
		return nil, err
	}
	g, err := s.store.RenameLocationGroup(ctx, id, name)
	return g, conflictOr(err, "name", "group already exists")
}

func (s *Service) DeleteGroup(ctx context.Context, id int64) error {
	err := s.store.DeleteLocationGroup(ctx, id)
	if errors.Is(err, store.ErrGroupNotEmpty) {
		return &ConflictError{Field: "group", Message: "group is in use: remove its members and move plans and pools off it first"}
	}
	return err
}

func (s *Service) SetGroupMembers(ctx context.Context, id int64, nodeIDs, inboundIDs []int64) error {
	if len(nodeIDs)+len(inboundIDs) > maxGroupMembers {
		return fieldErr("members", "too many members")
	}
	return referenceOr(s.store.SetGroupMembers(ctx, id, nodeIDs, inboundIDs), "members", "node or inbound not found")
}

func (s *Service) GetGroup(ctx context.Context, id int64) (*model.LocationGroup, error) {
	return s.store.GetLocationGroup(ctx, id)
}

func validatePlan(p *model.Plan) error {
	if err := validName("name", p.Name); err != nil {
		return err
	}
	if p.PriceRub < 0 {
		return fieldErr("price_rub", "must not be negative")
	}
	if p.PeriodDays < 1 || p.PeriodDays > 3650 {
		return fieldErr("period_days", "must be 1-3650")
	}
	if p.TrafficLimitBytes != nil && *p.TrafficLimitBytes <= 0 {
		return fieldErr("traffic_limit_bytes", "must be positive or null for unlimited")
	}
	if p.DeviceLimit != nil && (*p.DeviceLimit < 1 || *p.DeviceLimit > 100) {
		return fieldErr("device_limit", "must be 1-100 or null for unlimited")
	}
	if p.DeviceLimit != nil && !p.HWIDRequired {
		return fieldErr("hwid_required", "must be true when device_limit is set")
	}
	if p.Trial && p.PriceRub != 0 {
		return fieldErr("price_rub", "must be 0 for a trial plan")
	}
	if p.Visibility != model.VisibilityPublic && p.Visibility != model.VisibilityPrivate {
		return fieldErr("visibility", "must be public or private")
	}
	if len(p.Pools) > maxPlanPools {
		return fieldErr("pools", "too many pools")
	}
	for _, pool := range p.Pools {
		if (pool.NodeID == nil) == (pool.GroupID == nil) {
			return fieldErr("pools", "each pool needs exactly one of node_id or group_id")
		}
		if pool.TrafficLimitBytes != nil && *pool.TrafficLimitBytes <= 0 {
			return fieldErr("pools", "traffic_limit_bytes must be positive or null for unlimited")
		}
		if err := validText("pools", pool.Name, 64); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) planGroupExists(ctx context.Context, p *model.Plan) error {
	if p.LocationGroupID == nil {
		return nil
	}
	if _, err := s.store.GetLocationGroup(ctx, *p.LocationGroupID); errors.Is(err, store.ErrNotFound) {
		return fieldErr("location_group_id", "group not found")
	} else if err != nil {
		return err
	}
	return nil
}

func planWriteErr(err error) error {
	if errors.Is(err, store.ErrTrialOnSale) {
		return &ConflictError{Field: "trial", Message: "another trial plan is on sale"}
	}
	if errors.Is(err, store.ErrPoolOverlap) {
		return fieldErr("pools", "pools overlap: an inbound would count towards two pools")
	}
	return referenceOr(conflictOr(err, "name", "plan already exists"), "pools", "unknown pool, node or group")
}

func (s *Service) CreatePlanChecked(ctx context.Context, p *model.Plan) error {
	return s.CreatePlanOnSale(ctx, p, true)
}

func (s *Service) CreatePlanOnSale(ctx context.Context, p *model.Plan, onSale bool) error {
	if err := validatePlan(p); err != nil {
		return err
	}
	if err := s.planGroupExists(ctx, p); err != nil {
		return err
	}
	return planWriteErr(s.store.CreatePlanWith(ctx, p, onSale))
}

type PlanPatch struct {
	Name              *string           `json:"name"`
	PriceRub          *int64            `json:"price_rub"`
	PeriodDays        *int              `json:"period_days"`
	TrafficLimitBytes Optional[int64]   `json:"traffic_limit_bytes"`
	LocationGroupID   Optional[int64]   `json:"location_group_id"`
	DeviceLimit       Optional[int]     `json:"device_limit"`
	OnSale            *bool             `json:"on_sale"`
	HWIDRequired      *bool             `json:"hwid_required"`
	Trial             *bool             `json:"trial"`
	Visibility        *string           `json:"visibility"`
	DedicatedIP       *bool             `json:"dedicated_ip"`
	Pools             *[]model.PlanPool `json:"pools"`
}

func (s *Service) UpdatePlan(ctx context.Context, id int64, patch PlanPatch) (*model.Plan, error) {
	p, err := s.store.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.Name != nil {
		p.Name = *patch.Name
	}
	if patch.PriceRub != nil {
		p.PriceRub = *patch.PriceRub
	}
	if patch.PeriodDays != nil {
		p.PeriodDays = *patch.PeriodDays
	}
	if patch.TrafficLimitBytes.Set {
		p.TrafficLimitBytes = patch.TrafficLimitBytes.Value
	}
	if patch.LocationGroupID.Set {
		p.LocationGroupID = patch.LocationGroupID.Value
	}
	if patch.DeviceLimit.Set {
		p.DeviceLimit = patch.DeviceLimit.Value
	}
	if patch.OnSale != nil {
		p.OnSale = *patch.OnSale
	}
	if patch.HWIDRequired != nil {
		p.HWIDRequired = *patch.HWIDRequired
	}
	if patch.Trial != nil {
		p.Trial = *patch.Trial
	}
	if patch.Visibility != nil {
		p.Visibility = *patch.Visibility
	}
	if patch.DedicatedIP != nil {
		p.DedicatedIP = *patch.DedicatedIP
	}
	if patch.Pools != nil {
		p.Pools = *patch.Pools
	}
	if err := validatePlan(p); err != nil {
		return nil, err
	}
	if err := s.planGroupExists(ctx, p); err != nil {
		return nil, err
	}
	if err := planWriteErr(s.store.UpdatePlan(ctx, p)); err != nil {
		return nil, err
	}
	return s.store.GetPlan(ctx, id)
}

func (s *Service) ListPlans(ctx context.Context) ([]store.PlanRow, error) {
	return s.store.ListAdminPlans(ctx)
}

func (s *Service) NodeTraffic(ctx context.Context, nodeID int64) (store.NodeTraffic, error) {
	if _, err := s.store.GetNode(ctx, nodeID); err != nil {
		return store.NodeTraffic{}, err
	}
	return s.store.NodeTraffic(ctx, nodeID)
}

func validateNode(n *model.Node) error {
	if !certCNPattern.MatchString(n.CertCN) {
		return fieldErr("cert_cn", "must be 1-64 characters of A-Z, a-z, 0-9, '.', '_' and '-'")
	}
	if err := validName("name", n.Name); err != nil {
		return err
	}
	if !inbound.IsAddress(n.Address) {
		return fieldErr("address", "must be a hostname or an IP")
	}
	return nil
}

func (s *Service) nodeHosts(ctx context.Context) ([]string, error) {
	addrs, err := s.store.NodeAddresses(ctx)
	for i := range addrs {
		addrs[i] = inbound.CanonicalHost(addrs[i])
	}
	return addrs, err
}

func (s *Service) addressFreeOfDests(ctx context.Context, address string) error {
	dests, err := s.store.RealityDests(ctx)
	if err != nil {
		return err
	}
	for _, d := range dests {
		if inbound.DestHost(d) == inbound.CanonicalHost(address) {
			return fieldErr("address", "is used as reality.dest of an inbound: REALITY would loop into the node")
		}
	}
	return nil
}

func (s *Service) destNotANode(ctx context.Context, spec *inbound.Spec) error {
	if spec.Reality == nil {
		return nil
	}
	addrs, err := s.nodeHosts(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(addrs, inbound.DestHost(spec.Reality.Dest)) {
		return fieldErr("reality.dest", "points to one of our nodes: REALITY would loop into the node")
	}
	return nil
}

func (s *Service) CreateNode(ctx context.Context, n *model.Node) error {
	if err := validateNode(n); err != nil {
		return err
	}
	if err := s.addressFreeOfDests(ctx, n.Address); err != nil {
		return err
	}
	err := s.store.CreateNode(ctx, n)
	return referenceOr(conflictOr(err, "cert_cn", "node with this certificate already exists"), "group_ids", "group not found")
}

type NodePatch struct {
	Name      *string         `json:"name"`
	Address   *string         `json:"address"`
	GroupIDs  *[]int64        `json:"group_ids"`
	Enabled   *bool           `json:"enabled"`
	ProfileID Optional[int64] `json:"profile_id"`
}

func (s *Service) UpdateNode(ctx context.Context, id int64, patch NodePatch) (*model.Node, error) {
	n, err := s.store.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.Name != nil {
		if err := validName("name", *patch.Name); err != nil {
			return nil, err
		}
		n.Name = *patch.Name
	}
	if patch.Address != nil {
		if !inbound.IsAddress(*patch.Address) {
			return nil, fieldErr("address", "must be a hostname or an IP")
		}
		if err := s.addressFreeOfDests(ctx, *patch.Address); err != nil {
			return nil, err
		}
		n.Address = *patch.Address
	}
	n.GroupIDs = nil
	if patch.GroupIDs != nil {
		n.GroupIDs = nonNilIDs(*patch.GroupIDs)
	}
	exitChanged := (patch.Enabled != nil && *patch.Enabled != n.Enabled) || (patch.Address != nil && *patch.Address != n.Address)
	if patch.Enabled != nil {
		n.Enabled = *patch.Enabled
	}
	profileChanged := false
	if patch.ProfileID.Set {
		if patch.ProfileID.Value != nil {
			if err := s.profileFitsNode(ctx, *patch.ProfileID.Value, id); err != nil {
				return nil, err
			}
		}
		profileChanged = !slices.Equal(ptrSlice(n.ProfileID), ptrSlice(patch.ProfileID.Value))
		n.ProfileID = patch.ProfileID.Value
	}
	if err := s.store.UpdateNode(ctx, n); err != nil {
		if errors.Is(err, store.ErrInvalidReference) && patch.GroupIDs != nil {
			return nil, fieldErr("group_ids", "group not found")
		}
		return nil, referenceOr(err, "profile_id", "profile not found")
	}
	if profileChanged || (patch.Enabled != nil && exitChanged) {
		if err := s.store.RefreshLinks(ctx); err != nil {
			return nil, err
		}
	}
	if exitChanged {
		if err := s.store.BumpNodesCascadingTo(ctx, id); err != nil {
			return nil, err
		}
	}
	return s.GetNode(ctx, id)
}

func (s *Service) ListNodes(ctx context.Context) ([]model.Node, error) {
	list, err := s.store.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	return list, s.markNodes(ctx, list)
}

func (s *Service) GetNode(ctx context.Context, id int64) (*model.Node, error) {
	n, err := s.store.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	nodes := []model.Node{*n}
	if err := s.markNodes(ctx, nodes); err != nil {
		return nil, err
	}
	return &nodes[0], nil
}

type InboundView struct {
	ID        int64        `json:"id"`
	NodeID    int64        `json:"node_id"`
	Tag       string       `json:"tag"`
	Port      int          `json:"port"`
	Protocol  string       `json:"protocol"`
	Transport string       `json:"transport"`
	Security  string       `json:"security"`
	Flow      string       `json:"flow"`
	Label     string       `json:"label"`
	Reality   *RealityView `json:"reality,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

type RealityView struct {
	Dest        string   `json:"dest"`
	ServerNames []string `json:"server_names"`
	ShortIDs    []string `json:"short_ids"`
	Fingerprint string   `json:"fingerprint"`
	PublicKey   string   `json:"public_key"`
}

func SpecOf(in *model.NodeInbound) (inbound.Spec, error) {
	spec := inbound.Spec{
		Tag: in.Tag, Port: in.Port, Protocol: in.Protocol, Transport: in.Transport,
		Security: in.Security, Label: in.Label,
	}
	if in.Security == inbound.SecurityReality {
		var r inbound.Reality
		if err := json.Unmarshal(in.Settings, &r); err != nil {
			return spec, fmt.Errorf("inbound %d settings: %w", in.ID, err)
		}
		spec.Reality = &r
	}
	return spec, nil
}

func ViewOf(in *model.NodeInbound) (*InboundView, error) {
	spec, err := SpecOf(in)
	if err != nil {
		return nil, err
	}
	v := &InboundView{
		ID: in.ID, NodeID: in.NodeID, Tag: in.Tag, Port: in.Port, Protocol: in.Protocol,
		Transport: in.Transport, Security: in.Security, Flow: inbound.Flow(in.Transport, in.Security),
		Label: in.Label, CreatedAt: in.CreatedAt,
	}
	if spec.Reality != nil {
		pub, err := inbound.PublicKey(spec.Reality.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("inbound %d key: %w", in.ID, err)
		}
		v.Reality = &RealityView{
			Dest: spec.Reality.Dest, ServerNames: spec.Reality.ServerNames, ShortIDs: spec.Reality.ShortIDs,
			Fingerprint: spec.Reality.Fingerprint, PublicKey: pub,
		}
	}
	return v, nil
}

func sniGuard(names []string) store.InboundGuard {
	return func(others []model.NodeInbound) error {
		for _, o := range others {
			spec, err := SpecOf(&o)
			if err != nil {
				return err
			}
			for _, n := range inbound.ServerNames(&spec) {
				if slices.Contains(names, n) {
					return &ConflictError{Field: "reality.server_names", Message: fmt.Sprintf("%s is already used by inbound %s of this node", n, o.Tag)}
				}
			}
		}
		return nil
	}
}

func modelOf(nodeID int64, spec *inbound.Spec) (*model.NodeInbound, error) {
	settings := json.RawMessage(`{}`)
	if spec.Reality != nil {
		raw, err := json.Marshal(spec.Reality)
		if err != nil {
			return nil, err
		}
		settings = raw
	}
	return &model.NodeInbound{
		NodeID: nodeID, Tag: spec.Tag, Port: spec.Port, Protocol: spec.Protocol, Transport: spec.Transport,
		Security: spec.Security, Label: spec.Label, Settings: settings,
	}, nil
}

func (s *Service) CreateInbound(ctx context.Context, nodeID int64, spec inbound.Spec) (*InboundView, error) {
	if _, err := s.store.GetNode(ctx, nodeID); err != nil {
		return nil, err
	}
	if err := inbound.Normalize(&spec); err != nil {
		return nil, err
	}
	if err := s.destNotANode(ctx, &spec); err != nil {
		return nil, err
	}
	in, err := modelOf(nodeID, &spec)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateInbound(ctx, in, sniGuard(inbound.ServerNames(&spec))); err != nil {
		if errors.Is(err, store.ErrInvalidReference) {
			return nil, store.ErrNotFound
		}
		return nil, conflictOr(err, "tag", "tag already used on this node")
	}
	return ViewOf(in)
}

type RealityPatch struct {
	Dest        *string   `json:"dest"`
	ServerNames *[]string `json:"server_names"`
	ShortIDs    *[]string `json:"short_ids"`
	Fingerprint *string   `json:"fingerprint"`
	PrivateKey  *string   `json:"private_key"`
}

type InboundPatch struct {
	Tag     *string       `json:"tag"`
	Port    *int          `json:"port"`
	Label   *string       `json:"label"`
	Reality *RealityPatch `json:"reality"`
}

func applyInboundPatch(spec *inbound.Spec, patch InboundPatch) error {
	if patch.Tag != nil {
		spec.Tag = *patch.Tag
	}
	if patch.Port != nil {
		spec.Port = *patch.Port
	}
	if patch.Label != nil {
		spec.Label = *patch.Label
	}
	rp := patch.Reality
	if rp == nil {
		return nil
	}
	if spec.Reality == nil {
		return fieldErr("reality", "inbound has no reality settings")
	}
	if rp.Dest != nil {
		spec.Reality.Dest = *rp.Dest
	}
	if rp.ServerNames != nil {
		spec.Reality.ServerNames = *rp.ServerNames
	}
	if rp.ShortIDs != nil {
		spec.Reality.ShortIDs = *rp.ShortIDs
	}
	if rp.Fingerprint != nil {
		spec.Reality.Fingerprint = *rp.Fingerprint
	}
	if rp.PrivateKey != nil {
		spec.Reality.PrivateKey = *rp.PrivateKey
	}
	return nil
}

func (s *Service) UpdateInbound(ctx context.Context, id int64, patch InboundPatch) (*InboundView, error) {
	addrs, err := s.nodeHosts(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := s.store.ModifyInbound(ctx, id, func(current *model.NodeInbound, others []model.NodeInbound) (*model.NodeInbound, error) {
		spec, err := SpecOf(current)
		if err != nil {
			return nil, err
		}
		if err := applyInboundPatch(&spec, patch); err != nil {
			return nil, err
		}
		if err := inbound.Normalize(&spec); err != nil {
			return nil, err
		}
		if slices.Contains(addrs, inbound.DestHost(spec.Reality.Dest)) {
			return nil, fieldErr("reality.dest", "points to one of our nodes: REALITY would loop into the node")
		}
		if err := sniGuard(inbound.ServerNames(&spec))(others); err != nil {
			return nil, err
		}
		return modelOf(current.NodeID, &spec)
	})
	if err != nil {
		return nil, conflictOr(err, "tag", "tag already used on this node")
	}
	return ViewOf(updated)
}

func (s *Service) DeleteInbound(ctx context.Context, id int64) error {
	return s.store.DeleteInbound(ctx, id)
}

func (s *Service) InboundRefs(ctx context.Context) ([]store.InboundRef, error) {
	return s.store.InboundRefs(ctx)
}

func (s *Service) ListInbounds(ctx context.Context, nodeID int64) ([]InboundView, error) {
	if _, err := s.store.GetNode(ctx, nodeID); err != nil {
		return nil, err
	}
	list, err := s.store.ListNodeInbounds(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	out := make([]InboundView, 0, len(list))
	for i := range list {
		v, err := ViewOf(&list[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}
