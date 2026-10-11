package service

import (
	"context"
	"log/slog"

	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

type NodeUser struct {
	UUID string `json:"uuid"`
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
	Version  int64         `json:"version"`
	Users    []NodeUser    `json:"users"`
	Inbounds []SyncInbound `json:"inbounds"`
}

func (s *Service) SyncNodeUsers(ctx context.Context, nodeCN string, knownVersion int64) (*SyncResponse, error) {
	node, err := s.store.GetNodeByCN(ctx, nodeCN)
	if err != nil {
		return nil, err
	}
	resp, err := s.syncResponse(ctx, node, knownVersion)
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

func (s *Service) syncResponse(ctx context.Context, node *model.Node, knownVersion int64) (*SyncResponse, error) {
	if node.UsersVersion == knownVersion {
		return &SyncResponse{Version: node.UsersVersion}, nil
	}
	keys, err := s.store.ListNodeUserKeys(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	nodeUsers := make([]NodeUser, 0, len(keys))
	for _, k := range keys {
		nodeUsers = append(nodeUsers, NodeUser{UUID: k})
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
	return &SyncResponse{Version: node.UsersVersion, Users: nodeUsers, Inbounds: inbounds}, nil
}
