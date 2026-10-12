package store

import (
	"context"
	"time"
)

type CleanupResult struct {
	Deltas  int64 `json:"deltas"`
	Batches int64 `json:"batches"`
	Keys    int64 `json:"keys"`
}

func (s *Store) CleanupHistory(ctx context.Context, cutoff time.Time, chunk int) (CleanupResult, error) {
	var res CleanupResult
	var err error
	if res.Deltas, err = s.deleteInChunks(ctx,
		`DELETE FROM traffic_deltas WHERE id IN (SELECT id FROM traffic_deltas WHERE reported_at < $1 ORDER BY id LIMIT $2)`,
		`SELECT EXISTS (SELECT 1 FROM traffic_deltas WHERE reported_at < $1)`, cutoff, chunk); err != nil {
		return res, err
	}
	if res.Batches, err = s.deleteInChunks(ctx,
		`DELETE FROM traffic_batches WHERE (node_id, batch_id) IN (SELECT node_id, batch_id FROM traffic_batches WHERE received_at < $1 LIMIT $2)`,
		`SELECT EXISTS (SELECT 1 FROM traffic_batches WHERE received_at < $1)`, cutoff, chunk); err != nil {
		return res, err
	}
	res.Keys, err = s.deleteInChunks(ctx,
		`DELETE FROM user_keys WHERE id IN (SELECT id FROM user_keys WHERE revoked_at < $1 ORDER BY id LIMIT $2)`,
		`SELECT EXISTS (SELECT 1 FROM user_keys WHERE revoked_at < $1)`, cutoff, chunk)
	return res, err
}

func (s *Store) deleteInChunks(ctx context.Context, del string, exists string, cutoff time.Time, chunk int) (int64, error) {
	var total int64
	for {
		var found bool
		if err := s.pool.QueryRow(ctx, exists, cutoff).Scan(&found); err != nil {
			return total, err
		}
		if !found {
			return total, nil
		}
		tag, err := s.pool.Exec(ctx, del, cutoff, chunk)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}
