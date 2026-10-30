package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

const inboundColumns = `id, node_id, tag, port, protocol, transport, security, label, settings, created_at`

type InboundGuard func(others []model.NodeInbound) error

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func scanInbound(row pgxRow) (*model.NodeInbound, error) {
	in := &model.NodeInbound{}
	err := row.Scan(&in.ID, &in.NodeID, &in.Tag, &in.Port, &in.Protocol, &in.Transport, &in.Security, &in.Label, &in.Settings, &in.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return in, nil
}

func listInbounds(ctx context.Context, q querier, nodeID int64) ([]model.NodeInbound, error) {
	rows, err := q.Query(ctx, `SELECT `+inboundColumns+` FROM node_inbounds WHERE node_id = $1 ORDER BY tag`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.NodeInbound
	for rows.Next() {
		in, err := scanInbound(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *in)
	}
	return out, rows.Err()
}

func (s *Store) withLockedNode(ctx context.Context, nodeID int64, exclude int64, guard InboundGuard, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM nodes WHERE id = $1 FOR UPDATE`, nodeID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if guard != nil {
		all, err := listInbounds(ctx, tx, nodeID)
		if err != nil {
			return err
		}
		others := make([]model.NodeInbound, 0, len(all))
		for _, in := range all {
			if in.ID != exclude {
				others = append(others, in)
			}
		}
		if err := guard(others); err != nil {
			return err
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateInbound(ctx context.Context, in *model.NodeInbound, guard InboundGuard) error {
	err := s.withLockedNode(ctx, in.NodeID, 0, guard, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`INSERT INTO node_inbounds (node_id, tag, port, protocol, transport, security, label, settings)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 RETURNING id, created_at`,
			in.NodeID, in.Tag, in.Port, in.Protocol, in.Transport, in.Security, in.Label, in.Settings,
		).Scan(&in.ID, &in.CreatedAt)
	})
	if errors.Is(err, ErrNotFound) {
		return ErrInvalidReference
	}
	return mapWriteErr(err)
}

type InboundChange func(current *model.NodeInbound, others []model.NodeInbound) (*model.NodeInbound, error)

func (s *Store) ModifyInbound(ctx context.Context, id int64, change InboundChange) (*model.NodeInbound, error) {
	var nodeID int64
	if err := s.pool.QueryRow(ctx, `SELECT node_id FROM node_inbounds WHERE id = $1`, id).Scan(&nodeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var result *model.NodeInbound
	err := s.withLockedNode(ctx, nodeID, 0, nil, func(tx pgx.Tx) error {
		current, err := scanInbound(tx.QueryRow(ctx, `SELECT `+inboundColumns+` FROM node_inbounds WHERE id = $1`, id))
		if err != nil {
			return err
		}
		all, err := listInbounds(ctx, tx, nodeID)
		if err != nil {
			return err
		}
		others := make([]model.NodeInbound, 0, len(all))
		for _, in := range all {
			if in.ID != id {
				others = append(others, in)
			}
		}
		next, err := change(current, others)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`UPDATE node_inbounds SET tag = $2, port = $3, label = $4, settings = $5 WHERE id = $1`,
			id, next.Tag, next.Port, next.Label, next.Settings)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		next.ID, next.NodeID, next.CreatedAt = current.ID, current.NodeID, current.CreatedAt
		result = next
		return nil
	})
	if err != nil {
		return nil, mapWriteErr(err)
	}
	return result, nil
}

func (s *Store) UpdateInbound(ctx context.Context, in *model.NodeInbound, guard InboundGuard) error {
	_, err := s.ModifyInbound(ctx, in.ID, func(_ *model.NodeInbound, others []model.NodeInbound) (*model.NodeInbound, error) {
		if guard != nil {
			if err := guard(others); err != nil {
				return nil, err
			}
		}
		next := *in
		return &next, nil
	})
	return err
}

func (s *Store) GetInbound(ctx context.Context, id int64) (*model.NodeInbound, error) {
	return scanInbound(s.pool.QueryRow(ctx, `SELECT `+inboundColumns+` FROM node_inbounds WHERE id = $1`, id))
}

func (s *Store) ListNodeInbounds(ctx context.Context, nodeID int64) ([]model.NodeInbound, error) {
	return listInbounds(ctx, s.pool, nodeID)
}

func (s *Store) DeleteInbound(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM node_inbounds WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) InboundCounts(ctx context.Context) (map[int64]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT node_id, count(*) FROM node_inbounds GROUP BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

type InboundRef struct {
	ID        int64  `json:"id"`
	NodeID    int64  `json:"node_id"`
	Tag       string `json:"tag"`
	Label     string `json:"label"`
	Port      int    `json:"port"`
	Transport string `json:"transport"`
	Security  string `json:"security"`
}

func (s *Store) InboundRefs(ctx context.Context) ([]InboundRef, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, node_id, tag, label, port, transport, security FROM node_inbounds ORDER BY node_id, tag`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[InboundRef])
}

type Endpoint struct {
	NodeName    string
	NodeAddress string
	Inbound     model.NodeInbound
}

func (s *Store) ListPeriodEndpoints(ctx context.Context, periodID int64) ([]Endpoint, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT n.name, n.address, i.id, i.node_id, i.tag, i.port, i.protocol, i.transport, i.security, i.label, i.settings, i.created_at
		 FROM subscriptions sub
		 JOIN plan_inbounds pi ON pi.plan_id = sub.plan_id
		 JOIN period_pools x ON x.period_id = sub.id AND x.pool_id IS NOT DISTINCT FROM pi.pool_id
		 JOIN node_inbounds i ON i.id = pi.inbound_id
		 JOIN nodes n ON n.id = i.node_id
		 WHERE sub.id = $1 AND n.enabled AND n.address <> ''
		   AND (x.limit_bytes IS NULL OR x.used_bytes < x.limit_bytes)
		 ORDER BY n.name, n.id, i.tag`,
		periodID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Endpoint
	for rows.Next() {
		var e Endpoint
		in := &e.Inbound
		if err := rows.Scan(&e.NodeName, &e.NodeAddress, &in.ID, &in.NodeID, &in.Tag, &in.Port, &in.Protocol, &in.Transport,
			&in.Security, &in.Label, &in.Settings, &in.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) NodeAddresses(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT lower(address) FROM nodes WHERE address <> ''`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (s *Store) RealityDests(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT coalesce(settings->>'dest', '') FROM node_inbounds`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
