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

func (s *Store) UpdateInbound(ctx context.Context, in *model.NodeInbound, guard InboundGuard) error {
	current, err := s.GetInbound(ctx, in.ID)
	if err != nil {
		return err
	}
	err = s.withLockedNode(ctx, current.NodeID, in.ID, guard, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE node_inbounds SET tag = $2, port = $3, label = $4, settings = $5 WHERE id = $1`,
			in.ID, in.Tag, in.Port, in.Label, in.Settings)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	return mapWriteErr(err)
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
