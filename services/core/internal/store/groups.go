package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

const groupColumns = `g.id, g.name,
	ARRAY(SELECT gn.node_id FROM group_nodes gn WHERE gn.group_id = g.id ORDER BY gn.node_id),
	ARRAY(SELECT gi.inbound_id FROM group_inbounds gi WHERE gi.group_id = g.id ORDER BY gi.inbound_id),
	g.created_at`

func scanGroup(row pgxRow) (*model.LocationGroup, error) {
	g := &model.LocationGroup{}
	err := row.Scan(&g.ID, &g.Name, &g.NodeIDs, &g.InboundIDs, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, mapWriteErr(err)
	}
	return g, nil
}

func (s *Store) CreateLocationGroup(ctx context.Context, name string) (*model.LocationGroup, error) {
	return scanGroup(s.pool.QueryRow(ctx,
		`WITH g AS (INSERT INTO location_groups (name) VALUES ($1) RETURNING id, name, created_at)
		 SELECT g.id, g.name, '{}'::BIGINT[], '{}'::BIGINT[], g.created_at FROM g`, name))
}

func (s *Store) GetLocationGroup(ctx context.Context, id int64) (*model.LocationGroup, error) {
	return scanGroup(s.pool.QueryRow(ctx, `SELECT `+groupColumns+` FROM location_groups g WHERE g.id = $1`, id))
}

func (s *Store) RenameLocationGroup(ctx context.Context, id int64, name string) (*model.LocationGroup, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE location_groups SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetLocationGroup(ctx, id)
}

func (s *Store) DeleteLocationGroup(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked int64
	err = tx.QueryRow(ctx, `SELECT id FROM location_groups WHERE id = $1 FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var used bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM group_nodes WHERE group_id = $1)
		     OR EXISTS (SELECT 1 FROM group_inbounds WHERE group_id = $1)
		     OR EXISTS (SELECT 1 FROM plans WHERE location_group_id = $1)
		     OR EXISTS (SELECT 1 FROM plan_pools WHERE group_id = $1)`,
		id).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrGroupNotEmpty
	}
	if _, err := tx.Exec(ctx, `DELETE FROM location_groups WHERE id = $1`, id); err != nil {
		if errors.Is(mapWriteErr(err), ErrInvalidReference) {
			return ErrGroupNotEmpty
		}
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListLocationGroups(ctx context.Context) ([]model.LocationGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+groupColumns+` FROM location_groups g ORDER BY g.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []model.LocationGroup
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, *g)
	}
	return groups, rows.Err()
}

func (s *Store) SetGroupMembers(ctx context.Context, groupID int64, nodeIDs, inboundIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked int64
	err = tx.QueryRow(ctx, `SELECT id FROM location_groups WHERE id = $1 FOR UPDATE`, groupID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	nodeIDs, inboundIDs = nonNil(nodeIDs), nonNil(inboundIDs)
	for _, q := range []struct {
		sql string
		ids []int64
	}{
		{`DELETE FROM group_nodes WHERE group_id = $1 AND NOT node_id = ANY($2)`, nodeIDs},
		{`DELETE FROM group_inbounds WHERE group_id = $1 AND NOT inbound_id = ANY($2)`, inboundIDs},
		{`INSERT INTO group_nodes (group_id, node_id) SELECT $1, unnest($2::BIGINT[]) ON CONFLICT DO NOTHING`, nodeIDs},
		{`INSERT INTO group_inbounds (group_id, inbound_id) SELECT $1, unnest($2::BIGINT[]) ON CONFLICT DO NOTHING`, inboundIDs},
	} {
		if _, err := tx.Exec(ctx, q.sql, groupID, q.ids); err != nil {
			return mapWriteErr(err)
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) SetNodeGroups(ctx context.Context, nodeID int64, groupIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := setNodeGroups(ctx, tx, nodeID, groupIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func setNodeGroups(ctx context.Context, tx pgx.Tx, nodeID int64, groupIDs []int64) error {
	groupIDs = nonNil(groupIDs)
	if _, err := tx.Exec(ctx, `DELETE FROM group_nodes WHERE node_id = $1 AND NOT group_id = ANY($2)`, nodeID, groupIDs); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO group_nodes (group_id, node_id) SELECT unnest($2::BIGINT[]), $1 ON CONFLICT DO NOTHING`, nodeID, groupIDs)
	return mapWriteErr(err)
}

func nonNil(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}
