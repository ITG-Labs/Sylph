package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

const nodeColumns = `id, cert_cn, name, group_id, address, users_version, enabled, config_error, last_heartbeat, agent_version, xray_version,
	active_users, agent_uptime_sec, xray_uptime_sec, last_sync_at, created_at`

func (s *Store) scanNode(row pgxRow) (*model.Node, error) {
	n := &model.Node{}
	err := row.Scan(&n.ID, &n.CertCN, &n.Name, &n.GroupID, &n.Address, &n.UsersVersion, &n.Enabled, &n.ConfigError, &n.LastHeartbeat, &n.AgentVersion, &n.XrayVersion,
		&n.ActiveUsers, &n.AgentUptimeSec, &n.XrayUptimeSec, &n.LastSyncAt, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Store) UpsertNode(ctx context.Context, n *model.Node) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO nodes (cert_cn, name, group_id, address)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (cert_cn) DO UPDATE SET name = $2, group_id = $3, address = $4
		 RETURNING id, users_version, enabled, created_at`,
		n.CertCN, n.Name, n.GroupID, n.Address,
	).Scan(&n.ID, &n.UsersVersion, &n.Enabled, &n.CreatedAt)
}

func (s *Store) CreateNode(ctx context.Context, n *model.Node) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO nodes (cert_cn, name, group_id, address, enabled)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, users_version, created_at`,
		n.CertCN, n.Name, n.GroupID, n.Address, n.Enabled,
	).Scan(&n.ID, &n.UsersVersion, &n.CreatedAt)
	return mapWriteErr(err)
}

func (s *Store) UpdateNode(ctx context.Context, n *model.Node) error {
	err := s.pool.QueryRow(ctx,
		`UPDATE nodes SET name = $2, group_id = $3, address = $4, enabled = $5
		 WHERE id = $1
		 RETURNING users_version`,
		n.ID, n.Name, n.GroupID, n.Address, n.Enabled,
	).Scan(&n.UsersVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return mapWriteErr(err)
}

func (s *Store) GetNode(ctx context.Context, id int64) (*model.Node, error) {
	return s.scanNode(s.pool.QueryRow(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id = $1`, id))
}

func (s *Store) GetNodeByCN(ctx context.Context, cn string) (*model.Node, error) {
	return s.scanNode(s.pool.QueryRow(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE cert_cn = $1`, cn))
}

func (s *Store) ListNodes(ctx context.Context) ([]model.Node, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+nodeColumns+` FROM nodes ORDER BY id`)
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

func (s *Store) TouchNodeHeartbeat(ctx context.Context, nodeID int64, hb model.NodeHeartbeat) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE nodes SET last_heartbeat = now(), agent_version = $2, xray_version = NULLIF($3, ''),
		 agent_uptime_sec = $4, xray_uptime_sec = $5, active_users = $6, config_error = $7
		 WHERE id = $1`,
		nodeID, hb.AgentVersion, hb.XrayVersion, hb.AgentUptimeSec, hb.XrayUptimeSec, hb.ActiveUsers, hb.ConfigError)
	return err
}

func (s *Store) TouchNodeSync(ctx context.Context, nodeID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE nodes SET last_sync_at = now() WHERE id = $1`, nodeID)
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
		   AND (n.group_id IS NULL OR p.location_group_id = n.group_id)`,
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
