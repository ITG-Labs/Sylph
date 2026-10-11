package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

const inboundColumns = `id, node_id, tag, port, protocol, transport, security, label, settings, created_at`

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

func (s *Store) CreateInbound(ctx context.Context, in *model.NodeInbound) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO node_inbounds (node_id, tag, port, protocol, transport, security, label, settings)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, created_at`,
		in.NodeID, in.Tag, in.Port, in.Protocol, in.Transport, in.Security, in.Label, in.Settings,
	).Scan(&in.ID, &in.CreatedAt)
	return mapWriteErr(err)
}

func (s *Store) GetInbound(ctx context.Context, id int64) (*model.NodeInbound, error) {
	return scanInbound(s.pool.QueryRow(ctx, `SELECT `+inboundColumns+` FROM node_inbounds WHERE id = $1`, id))
}

func (s *Store) ListNodeInbounds(ctx context.Context, nodeID int64) ([]model.NodeInbound, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+inboundColumns+` FROM node_inbounds WHERE node_id = $1 ORDER BY tag`, nodeID)
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

func (s *Store) UpdateInbound(ctx context.Context, in *model.NodeInbound) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE node_inbounds SET tag = $2, port = $3, label = $4, settings = $5 WHERE id = $1`,
		in.ID, in.Tag, in.Port, in.Label, in.Settings)
	if err != nil {
		return mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
