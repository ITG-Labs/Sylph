package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func (s *Store) scanNode(row pgxRow) (*model.Node, error) {
	n := &model.Node{}
	err := row.Scan(&n.ID, &n.CertCN, &n.Name, &n.GroupID, &n.Config, &n.UsersVersion, &n.Enabled, &n.LastHeartbeat, &n.AgentVersion, &n.XrayVersion, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Store) UpsertNode(ctx context.Context, n *model.Node) error {
	config := n.Config
	if config == "" {
		config = "{}"
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO nodes (cert_cn, name, group_id, config)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (cert_cn) DO UPDATE SET name = $2, group_id = $3, config = $4
		 RETURNING id, users_version, enabled, created_at`,
		n.CertCN, n.Name, n.GroupID, config,
	).Scan(&n.ID, &n.UsersVersion, &n.Enabled, &n.CreatedAt)
}

func (s *Store) GetNodeByCN(ctx context.Context, cn string) (*model.Node, error) {
	return s.scanNode(s.pool.QueryRow(ctx,
		`SELECT id, cert_cn, name, group_id, config, users_version, enabled, last_heartbeat, agent_version, xray_version, created_at
		 FROM nodes WHERE cert_cn = $1`, cn))
}

func (s *Store) ListNodes(ctx context.Context) ([]model.Node, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, cert_cn, name, group_id, config, users_version, enabled, last_heartbeat, agent_version, xray_version, created_at
		 FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []model.Node
	for rows.Next() {
		n, err := s.scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, *n)
	}
	return nodes, rows.Err()
}

func (s *Store) UpdateNodeConfig(ctx context.Context, nodeID int64, config string) error {
	_, err := s.pool.Exec(ctx, `UPDATE nodes SET config = $2 WHERE id = $1`, nodeID, config)
	return err
}

func (s *Store) TouchNodeHeartbeat(ctx context.Context, nodeID int64, agentVer string, xrayVer string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE nodes SET last_heartbeat = now(), agent_version = $2, xray_version = $3 WHERE id = $1`,
		nodeID, agentVer, xrayVer)
	return err
}
