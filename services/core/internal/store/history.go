package store

import (
	"context"
	"time"
)

type CleanupResult struct {
	Deltas        int64 `json:"deltas"`
	Batches       int64 `json:"batches"`
	Keys          int64 `json:"keys"`
	Daily         int64 `json:"daily"`
	Notifications int64 `json:"notifications"`
	IPs           int64 `json:"ips"`
	Partial       bool  `json:"partial"`
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

var cleanupStepBudget = 3 * time.Second

func (s *Store) CleanupHistory(ctx context.Context, cutoff time.Time, chunk int) (CleanupResult, error) {
	var res CleanupResult
	var err error
	var partial bool
	if res.Notifications, partial, err = s.cleanupNotifications(ctx, cutoff, chunk); err != nil {
		return res, err
	}
	res.Partial = partial
	if res.Keys, partial, err = s.deleteInChunks(ctx, keysCleanup, cutoff, chunk); err != nil {
		return res, err
	}
	res.Partial = res.Partial || partial
	if res.Batches, partial, err = s.deleteInChunks(ctx, batchesCleanup, cutoff, chunk); err != nil {
		return res, err
	}
	res.Partial = res.Partial || partial
	if res.Deltas, partial, err = s.deleteInChunks(ctx, deltasCleanup, cutoff, chunk); err != nil {
		return res, err
	}
	res.Partial = res.Partial || partial
	return res, nil
}

func (s *Store) deleteInChunks(ctx context.Context, q chunkedDelete, cutoff time.Time, chunk int) (int64, bool, error) {
	deadline := time.Now().Add(cleanupStepBudget)
	var total int64
	for {
		var bound any
		if err := s.pool.QueryRow(ctx, q.bound, cutoff, chunk).Scan(&bound); err != nil {
			return total, false, err
		}
		if bound == nil {
			return total, false, nil
		}
		tag, err := s.pool.Exec(ctx, q.del, cutoff, bound)
		if err != nil {
			return total, false, err
		}
		n := tag.RowsAffected()
		total += n
		if n < int64(chunk) {
			return total, false, nil
		}
		if !time.Now().Before(deadline) {
			return total, true, nil
		}
	}
}

func (s *Store) deleteBudgeted(ctx context.Context, stmt string, chunk int, args ...any) (int64, bool, error) {
	deadline := time.Now().Add(cleanupStepBudget)
	args = append(args, chunk)
	var total int64
	for {
		tag, err := s.pool.Exec(ctx, stmt, args...)
		if err != nil {
			return total, false, err
		}
		n := tag.RowsAffected()
		total += n
		if n < int64(chunk) {
			return total, false, nil
		}
		if !time.Now().Before(deadline) {
			return total, true, nil
		}
	}
}
