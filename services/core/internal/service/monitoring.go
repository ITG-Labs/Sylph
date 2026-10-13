package service

import (
	"context"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	NodeOnline   = "online"
	NodeDegraded = "degraded"
	NodeOffline  = "offline"
	NodeNever    = "never"
	NodeDisabled = "disabled"

	DefaultNodeOffline   = time.Minute
	DefaultNodeSyncStale = 3 * time.Minute
)

func (s *Service) SetNodeThresholds(offline, stale time.Duration) {
	s.nodeOffline, s.nodeStale = offline, stale
}

func (s *Service) nodeStatus(n *model.Node, now time.Time) string {
	switch {
	case !n.Enabled:
		return NodeDisabled
	case n.LastHeartbeat == nil:
		return NodeNever
	case now.Sub(*n.LastHeartbeat) > s.nodeOffline:
		return NodeOffline
	case n.LastSyncAt == nil || now.Sub(*n.LastSyncAt) > s.nodeStale || n.ConfigError != nil:
		return NodeDegraded
	}
	return NodeOnline
}

func (s *Service) markNodes(ctx context.Context, nodes []model.Node) error {
	if len(nodes) == 0 {
		return nil
	}
	now, err := s.store.DBNow(ctx)
	if err != nil {
		return err
	}
	for i := range nodes {
		nodes[i].Status = s.nodeStatus(&nodes[i], now)
	}
	return nil
}

type NodeSummary struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	ActiveUsers  *int   `json:"active_users"`
	TrafficToday int64  `json:"traffic_today"`
}

type NodesSummary struct {
	Total       int            `json:"total"`
	ByStatus    map[string]int `json:"by_status"`
	ActiveUsers int            `json:"active_users"`
	Nodes       []NodeSummary  `json:"nodes"`
}

type Attention struct {
	store.AttentionCounts
	NodesProblem int `json:"nodes_problem"`
}

type Overview struct {
	Nodes     NodesSummary         `json:"nodes"`
	Users     store.UsersSummary   `json:"users"`
	Revenue   store.RevenueSummary `json:"revenue"`
	Attention Attention            `json:"attention"`
}

func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.OverviewCounts(ctx, time.Now().Add(-pollWindow))
	if err != nil {
		return nil, err
	}
	ov := &Overview{
		Nodes: NodesSummary{
			Total:    len(nodes),
			ByStatus: map[string]int{NodeOnline: 0, NodeDegraded: 0, NodeOffline: 0, NodeNever: 0, NodeDisabled: 0},
			Nodes:    make([]NodeSummary, 0, len(nodes)),
		},
		Users:     counts.Users,
		Revenue:   counts.Revenue,
		Attention: Attention{AttentionCounts: counts.Attention},
	}
	for _, n := range nodes {
		ov.Nodes.ByStatus[n.Status]++
		if n.ActiveUsers != nil && n.Status != NodeDisabled {
			ov.Nodes.ActiveUsers += *n.ActiveUsers
		}
		if n.Status == NodeOffline || n.Status == NodeDegraded {
			ov.Attention.NodesProblem++
		}
		ov.Nodes.Nodes = append(ov.Nodes.Nodes, NodeSummary{ID: n.ID, Name: n.Name, Status: n.Status, ActiveUsers: n.ActiveUsers, TrafficToday: counts.TrafficToday[n.ID]})
	}
	return ov, nil
}
