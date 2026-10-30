package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

const nodeColumns = `id, cert_cn, name, ARRAY(SELECT gn.group_id FROM group_nodes gn WHERE gn.node_id = nodes.id ORDER BY gn.group_id), address, users_version, enabled, config_error, last_heartbeat, agent_version, xray_version,
	active_users, agent_uptime_sec, xray_uptime_sec, last_sync_at, agent_per_inbound, agent_xhttp, profile_id, geo_status, routing_error, cert_not_after, tls_status, created_at`

func (s *Store) scanNode(row pgxRow) (*model.Node, error) {
	n := &model.Node{}
	err := row.Scan(&n.ID, &n.CertCN, &n.Name, &n.GroupIDs, &n.Address, &n.UsersVersion, &n.Enabled, &n.ConfigError, &n.LastHeartbeat, &n.AgentVersion, &n.XrayVersion,
		&n.ActiveUsers, &n.AgentUptimeSec, &n.XrayUptimeSec, &n.LastSyncAt, &n.AgentPerInbound, &n.AgentXHTTP, &n.ProfileID, &n.GeoStatus, &n.RoutingError, &n.CertNotAfter, &n.TLSStatus, &n.CreatedAt)
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
		`INSERT INTO nodes (cert_cn, name, address)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (cert_cn) DO UPDATE SET name = $2, address = $3
		 RETURNING id, users_version, enabled, created_at`,
		n.CertCN, n.Name, n.Address,
	).Scan(&n.ID, &n.UsersVersion, &n.Enabled, &n.CreatedAt)
}

func (s *Store) CreateNode(ctx context.Context, n *model.Node) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx,
		`INSERT INTO nodes (cert_cn, name, address, enabled)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, users_version, created_at`,
		n.CertCN, n.Name, n.Address, n.Enabled,
	).Scan(&n.ID, &n.UsersVersion, &n.CreatedAt)
	if err != nil {
		return mapWriteErr(err)
	}
	if n.GroupIDs != nil {
		if err := setNodeGroups(ctx, tx, n.ID, n.GroupIDs); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) UpdateNode(ctx context.Context, n *model.Node) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if n.GroupIDs != nil {
		if err := setNodeGroups(ctx, tx, n.ID, n.GroupIDs); err != nil {
			return err
		}
	}
	err = tx.QueryRow(ctx,
		`UPDATE nodes SET name = $2, address = $3, enabled = $4, profile_id = $5
		 WHERE id = $1
		 RETURNING users_version`,
		n.ID, n.Name, n.Address, n.Enabled, n.ProfileID,
	).Scan(&n.UsersVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return mapWriteErr(err)
	}
	return tx.Commit(ctx)
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
		 agent_uptime_sec = $4, xray_uptime_sec = $5, active_users = $6, config_error = $7, geo_status = $8, routing_error = $9,
		 cert_not_after = COALESCE($10, cert_not_after), tls_status = $11
		 WHERE id = $1`,
		nodeID, hb.AgentVersion, hb.XrayVersion, hb.AgentUptimeSec, hb.XrayUptimeSec, hb.ActiveUsers, hb.ConfigError, nullJSON(hb.GeoStatus), hb.RoutingError, hb.CertNotAfter, nullJSON(hb.TLSStatus))
	return err
}

func (s *Store) TouchNodeSync(ctx context.Context, nodeID int64, perInbound, xhttp bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE nodes SET last_sync_at = now(), agent_per_inbound = $2, agent_xhttp = $3 WHERE id = $1`, nodeID, perInbound, xhttp)
	return err
}

func (s *Store) ListNodeUserKeys(ctx context.Context, nodeID int64) ([]string, error) {
	keys, err := s.ListNodeKeys(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.UUID)
	}
	return out, nil
}

func (s *Store) DBNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := s.pool.QueryRow(ctx, `SELECT now()`).Scan(&now)
	return now, err
}
