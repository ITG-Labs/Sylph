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

var cleanupStepBudget = 6 * time.Second

func (s *Store) CleanupHistory(ctx context.Context, cutoff time.Time, chunk int) (CleanupResult, error) {
	var res CleanupResult
	var err error
	if res.Notifications, err = s.cleanupNotifications(ctx, cutoff); err != nil {
		return res, err
	}
	var partial bool
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
	stepCtx, cancel := context.WithTimeout(ctx, cleanupStepBudget)
	defer cancel()
	var total int64
	for {
		if stepCtx.Err() != nil {
			return total, ctx.Err() == nil, ctx.Err()
		}
		var bound any
		if err := s.pool.QueryRow(stepCtx, q.bound, cutoff, chunk).Scan(&bound); err != nil {
			return total, budgetSpent(ctx, stepCtx), budgetErr(ctx, stepCtx, err)
		}
		if bound == nil {
			return total, false, nil
		}
		tag, err := s.pool.Exec(stepCtx, q.del, cutoff, bound)
		if err != nil {
			return total, budgetSpent(ctx, stepCtx), budgetErr(ctx, stepCtx, err)
		}
		if tag.RowsAffected() == 0 {
			return total, false, nil
		}
		total += tag.RowsAffected()
	}
}

func budgetSpent(ctx, stepCtx context.Context) bool {
	return ctx.Err() == nil && stepCtx.Err() != nil
}

func budgetErr(ctx, stepCtx context.Context, err error) error {
	if budgetSpent(ctx, stepCtx) {
		return nil
	}
	return err
}
