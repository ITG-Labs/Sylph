package store

import (
	"context"
	"errors"
	"math"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

type Exhaustion struct {
	UserID int64
	Used   int64
	Limit  int64
}

type BatchResult struct {
	Duplicate bool
	Exhausted []Exhaustion
}

func (s *Store) RecordDeltaBatch(ctx context.Context, nodeID int64, batchID string, deltas []model.TrafficDelta) (BatchResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BatchResult{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx,
		`INSERT INTO traffic_batches (node_id, batch_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		nodeID, batchID)
	if err != nil {
		return BatchResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return BatchResult{Duplicate: true}, nil
	}
	owners, err := keyOwners(ctx, tx, deltas)
	if err != nil {
		return BatchResult{}, err
	}
	totals := map[int64]*userTotal{}
	for _, d := range deltas {
		owner, ok := owners[d.KeyUUID]
		var userID *int64
		if ok {
			userID = &owner
			t := totals[owner]
			if t == nil {
				t = &userTotal{}
				totals[owner] = t
			}
			t.up, t.down = satAdd(t.up, d.BytesUp), satAdd(t.down, d.BytesDown)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO traffic_deltas (node_id, key_uuid, user_id, bytes_up, bytes_down) VALUES ($1, $2, $3, $4, $5)`,
			nodeID, d.KeyUUID, userID, d.BytesUp, d.BytesDown); err != nil {
			return BatchResult{}, err
		}
	}
	users := make([]int64, 0, len(totals))
	for id := range totals {
		users = append(users, id)
	}
	slices.Sort(users)
	var res BatchResult
	for _, id := range users {
		t := totals[id]
		bytes := satAdd(t.up, t.down)
		periodID, before, used, limit, err := addToActivePeriod(ctx, tx, id, bytes)
		if errors.Is(err, pgx.ErrNoRows) {
			periodID, before, used, limit, err = addToActivePeriod(ctx, tx, id, bytes)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			periodID, limit, err = 0, nil, nil
		}
		if err != nil {
			return BatchResult{}, err
		}
		if err := s.addDaily(ctx, tx, id, nodeID, periodID, t.up, t.down); err != nil {
			return BatchResult{}, err
		}
		if limit != nil && before < *limit && used >= *limit {
			res.Exhausted = append(res.Exhausted, Exhaustion{UserID: id, Used: used, Limit: *limit})
		}
	}
	if len(res.Exhausted) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE nodes SET users_version = next_users_version(users_version)`); err != nil {
			return BatchResult{}, err
		}
	}
	return res, tx.Commit(ctx)
}

type userTotal struct {
	up, down int64
}

func addToActivePeriod(ctx context.Context, tx pgx.Tx, userID, bytes int64) (periodID, before, used int64, limit *int64, err error) {
	err = tx.QueryRow(ctx,
		`WITH cur AS (
		     SELECT id, traffic_used_bytes AS before FROM subscriptions
		     WHERE user_id = $1 AND NOT canceled AND starts_at <= now() AND expires_at > now()
		     ORDER BY starts_at DESC, id DESC LIMIT 1 FOR UPDATE)
		 UPDATE subscriptions s
		 SET traffic_used_bytes = LEAST(s.traffic_used_bytes::numeric + $2, 9223372036854775807)::bigint
		 FROM cur, plans p
		 WHERE s.id = cur.id AND p.id = s.plan_id
		 RETURNING s.id, cur.before, s.traffic_used_bytes, `+effectiveLimit,
		userID, bytes).Scan(&periodID, &before, &used, &limit)
	return periodID, before, used, limit, err
}

func keyOwners(ctx context.Context, tx pgx.Tx, deltas []model.TrafficDelta) (map[string]int64, error) {
	uuids := make([]string, 0, len(deltas))
	for _, d := range deltas {
		uuids = append(uuids, d.KeyUUID)
	}
	rows, err := tx.Query(ctx, `SELECT uuid, user_id FROM user_keys WHERE uuid = ANY($1)`, uuids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := make(map[string]int64, len(uuids))
	for rows.Next() {
		var uuid string
		var userID int64
		if err := rows.Scan(&uuid, &userID); err != nil {
			return nil, err
		}
		owners[uuid] = userID
	}
	return owners, rows.Err()
}

func satAdd(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
