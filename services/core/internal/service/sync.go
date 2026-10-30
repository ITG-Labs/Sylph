package service

import (
	"context"
	"log/slog"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

type NodeUser struct {
	UUID     string   `json:"uuid"`
	PlanID   int64    `json:"plan_id"`
	EgressIP string   `json:"egress_ip,omitempty"`
	Inbounds []string `json:"inbounds,omitempty"`
}

type SyncGeoSource struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	SHA256URL string `json:"sha256_url"`
}

type SyncGeo struct {
	Generation int64           `json:"generation"`
	Sources    []SyncGeoSource `json:"sources"`
}

type SyncInbound struct {
	Tag       string           `json:"tag"`
	Port      int              `json:"port"`
	Protocol  string           `json:"protocol"`
	Transport string           `json:"transport"`
	Security  string           `json:"security"`
	Flow      string           `json:"flow"`
	Reality   *inbound.Reality `json:"reality,omitempty"`
}

type SyncResponse struct {
	Version     int64              `json:"version"`
	Users       []NodeUser         `json:"users"`
	Inbounds    []SyncInbound      `json:"inbounds"`
	Routing     *noderoute.Profile `json:"routing,omitempty"`
	ServiceKeys []string           `json:"service_keys,omitempty"`
	Geo         *SyncGeo           `json:"geo,omitempty"`
}

func (s *Service) SyncNodeUsers(ctx context.Context, nodeCN string, knownVersion int64, perInbound bool) (*SyncResponse, error) {
	node, err := s.store.GetNodeByCN(ctx, nodeCN)
	if err != nil {
		return nil, err
	}
	resp, err := s.syncResponse(ctx, node, knownVersion, perInbound)
	if err != nil {
		return nil, err
	}
	if err := s.store.TouchNodeSync(ctx, node.ID); err != nil {
		s.log.LogAttrs(ctx, slog.LevelError, "node_sync_mark_failed",
			slog.String("node", nodeCN),
			slog.String("error", err.Error()))
	}
	return resp, nil
}

func (s *Service) syncResponse(ctx context.Context, node *model.Node, knownVersion int64, perInbound bool) (*SyncResponse, error) {
	if node.UsersVersion == knownVersion {
		return &SyncResponse{Version: node.UsersVersion}, nil
	}
	list := s.store.ListNodeKeysLegacy
	if perInbound {
		list = s.store.ListNodeKeys
	}
	keys, err := list(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	nodeUsers := make([]NodeUser, 0, len(keys))
	for _, k := range keys {
		u := NodeUser{UUID: k.UUID, PlanID: k.PlanID, EgressIP: k.EgressIP}
		if perInbound {
			u.Inbounds = k.Inbounds
		}
		nodeUsers = append(nodeUsers, u)
	}
	stored, err := s.store.ListNodeInbounds(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	inbounds := make([]SyncInbound, 0, len(stored))
	for i := range stored {
		spec, err := SpecOf(&stored[i])
		if err != nil {
			return nil, err
		}
		inbounds = append(inbounds, SyncInbound{
			Tag: spec.Tag, Port: spec.Port, Protocol: spec.Protocol, Transport: spec.Transport,
			Security: spec.Security, Flow: inbound.Flow(spec.Transport, spec.Security), Reality: spec.Reality,
		})
	}
	serviceKeys, err := s.store.ServiceKeys(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	routing, geo, err := s.nodeRouting(ctx, node)
	if err != nil {
		s.log.LogAttrs(ctx, slog.LevelError, "node_routing_failed", slog.Int64("node_id", node.ID), slog.String("error", err.Error()))
		return nil, err
	}
	return &SyncResponse{Version: node.UsersVersion, Users: nodeUsers, Inbounds: inbounds, Routing: routing, ServiceKeys: serviceKeys, Geo: geo}, nil
}
