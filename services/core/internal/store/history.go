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

type chunkedDelete struct {
	bound string
	del   string
}

var (
	deltasCleanup = chunkedDelete{
		bound: `SELECT max(id) FROM (SELECT id FROM traffic_deltas WHERE reported_at < $1 ORDER BY id LIMIT $2) s`,
		del:   `DELETE FROM traffic_deltas WHERE id <= $2 AND reported_at < $1`,
	}
	batchesCleanup = chunkedDelete{
		bound: `SELECT max(received_at) FROM (SELECT received_at FROM traffic_batches WHERE received_at < $1 ORDER BY received_at LIMIT $2) s`,
		del:   `DELETE FROM traffic_batches WHERE received_at <= $2 AND received_at < $1`,
	}
	keysCleanup = chunkedDelete{
		bound: `SELECT max(id) FROM (SELECT id FROM user_keys WHERE revoked_at < $1 ORDER BY id LIMIT $2) s`,
		del:   `DELETE FROM user_keys WHERE id <= $2 AND revoked_at < $1`,
	}
)

func (s *Store) CleanupHistory(ctx context.Context, cutoff time.Time, chunk int) (CleanupResult, error) {
	var res CleanupResult
	var err error
	if res.Deltas, err = s.deleteInChunks(ctx, deltasCleanup, cutoff, chunk); err != nil {
		return res, err
	}
	if res.Batches, err = s.deleteInChunks(ctx, batchesCleanup, cutoff, chunk); err != nil {
		return res, err
	}
	res.Keys, err = s.deleteInChunks(ctx, keysCleanup, cutoff, chunk)
	return res, err
}

func (s *Store) deleteInChunks(ctx context.Context, q chunkedDelete, cutoff time.Time, chunk int) (int64, error) {
	var total int64
	for {
		var bound any
		if err := s.pool.QueryRow(ctx, q.bound, cutoff, chunk).Scan(&bound); err != nil {
			return total, err
		}
		if bound == nil {
			return total, nil
		}
		tag, err := s.pool.Exec(ctx, q.del, cutoff, bound)
		if err != nil {
			return total, err
		}
		if tag.RowsAffected() == 0 {
			return total, nil
		}
		total += tag.RowsAffected()
	}
}
