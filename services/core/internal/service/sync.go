package service

import (
	"context"
)

type NodeUser struct {
	UUID string `json:"uuid"`
}

type SyncResponse struct {
	Version int64      `json:"version"`
	Users   []NodeUser `json:"users"`
	Config  string     `json:"config"`
}

func (s *Service) SyncNodeUsers(ctx context.Context, nodeCN string, knownVersion int64) (*SyncResponse, error) {
	node, err := s.store.GetNodeByCN(ctx, nodeCN)
	if err != nil {
		return nil, err
	}
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
	return &SyncResponse{Version: node.UsersVersion, Users: nodeUsers, Config: node.Config}, nil
}
