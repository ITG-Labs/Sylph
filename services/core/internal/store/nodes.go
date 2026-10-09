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

func (s *Store) ListNodeUserKeys(ctx context.Context, nodeID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT uk.uuid
		 FROM user_keys uk
		 JOIN users u ON u.id = uk.user_id
		 JOIN subscriptions sub ON sub.user_id = u.id AND NOT sub.canceled AND sub.expires_at > now()
		 JOIN plans p ON p.id = sub.plan_id
		 JOIN nodes n ON n.id = $1
		 WHERE u.status = 'active'
		   AND n.group_id IS NOT NULL
		   AND p.location_group_id = n.group_id`,
		nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
