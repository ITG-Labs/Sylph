package store

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

type Exhaustion struct {
	UserID int64
	PoolID *int64
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
				t = &userTotal{byInbound: map[string]int64{}}
				totals[owner] = t
			}
			t.up, t.down = satAdd(t.up, d.BytesUp), satAdd(t.down, d.BytesDown)
			t.byInbound[d.Inbound] = satAdd(t.byInbound[d.Inbound], satAdd(d.BytesUp, d.BytesDown))
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
	var resync []int64
	pools := &poolCache{node: nodeID, byTag: map[planTag]*int64{}, limits: map[int64]map[int64]*int64{}}
	for _, id := range users {
		t := totals[id]
		periodID, planID, err := lockActivePeriod(ctx, tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			periodID, planID, err = lockActivePeriod(ctx, tx, id)
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return BatchResult{}, err
		}
		if err := s.addDaily(ctx, tx, id, nodeID, periodID, t.up, t.down); err != nil {
			return BatchResult{}, err
		}
		if periodID == 0 {
			continue
		}
		totals, err := pools.totals(ctx, tx, planID, t.byInbound)
		if err != nil {
			return BatchResult{}, err
		}
		for _, pt := range totals {
			before, used, limit, err := pools.add(ctx, tx, periodID, planID, pt.pool, pt.bytes)
			if err != nil {
				return BatchResult{}, err
			}
			if limit != nil && before < *limit && used >= *limit {
				res.Exhausted = append(res.Exhausted, Exhaustion{UserID: id, PoolID: pt.pool, Used: used, Limit: *limit})
				nodes, err := poolNodes(ctx, tx, planID, pt.pool)
				if err != nil {
					return BatchResult{}, err
				}
				resync = append(resync, nodes...)
			}
		}
	}
	if len(resync) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE nodes SET users_version = next_users_version(users_version) WHERE id = ANY($1)`, resync); err != nil {
			return BatchResult{}, err
		}
	}
	return res, tx.Commit(ctx)
}

func lockActivePeriod(ctx context.Context, tx pgx.Tx, userID int64) (periodID, planID int64, err error) {
	err = tx.QueryRow(ctx,
		`SELECT id, plan_id FROM subscriptions
		 WHERE user_id = $1 AND NOT canceled AND starts_at <= now() AND expires_at > now()
		 ORDER BY starts_at DESC, id DESC LIMIT 1 FOR UPDATE`, userID).Scan(&periodID, &planID)
	return periodID, planID, err
}

type userTotal struct {
	up, down  int64
	byInbound map[string]int64
}

type poolTotal struct {
	pool  *int64
	bytes int64
}

type planTag struct {
	plan int64
	tag  string
}

type poolCache struct {
	node   int64
	byTag  map[planTag]*int64
	limits map[int64]map[int64]*int64
}

func (c *poolCache) poolOf(ctx context.Context, tx pgx.Tx, planID int64, tag string) (*int64, error) {
	key := planTag{planID, tag}
	if pool, ok := c.byTag[key]; ok {
		return pool, nil
	}
	var pool *int64
	err := tx.QueryRow(ctx,
		`SELECT pi.pool_id FROM plan_inbounds pi JOIN node_inbounds i ON i.id = pi.inbound_id
		 WHERE pi.plan_id = $1 AND pi.node_id = $2
		 ORDER BY i.tag = $3 DESC, i.id LIMIT 1`, planID, c.node, tag).Scan(&pool)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	c.byTag[key] = pool
	return pool, nil
}

func (c *poolCache) totals(ctx context.Context, tx pgx.Tx, planID int64, byInbound map[string]int64) ([]poolTotal, error) {
	tags := make([]string, 0, len(byInbound))
	for tag := range byInbound {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	var out []poolTotal
	for _, tag := range tags {
		pool, err := c.poolOf(ctx, tx, planID, tag)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(out, func(p poolTotal) bool { return samePool(p.pool, pool) })
		if i < 0 {
			out = append(out, poolTotal{pool: pool})
			i = len(out) - 1
		}
		out[i].bytes = satAdd(out[i].bytes, byInbound[tag])
	}
	slices.SortFunc(out, func(a, b poolTotal) int { return cmpPool(a.pool, b.pool) })
	return out, nil
}

func (c *poolCache) limitOf(ctx context.Context, tx pgx.Tx, planID, poolID int64) (*int64, error) {
	limits, ok := c.limits[planID]
	if !ok {
		rows, err := tx.Query(ctx, `SELECT id, traffic_limit_bytes FROM plan_pools WHERE plan_id = $1`, planID)
		if err != nil {
			return nil, err
		}
		limits = map[int64]*int64{}
		for rows.Next() {
			var id int64
			var limit *int64
			if err := rows.Scan(&id, &limit); err != nil {
				rows.Close()
				return nil, err
			}
			limits[id] = limit
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		c.limits[planID] = limits
	}
	return limits[poolID], nil
}

func samePool(a, b *int64) bool {
	return cmpPool(a, b) == 0
}

func cmpPool(a, b *int64) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return cmp.Compare(*a, *b)
}

func (c *poolCache) add(ctx context.Context, tx pgx.Tx, periodID, planID int64, pool *int64, bytes int64) (before, used int64, limit *int64, err error) {
	if pool == nil {
		err = tx.QueryRow(ctx,
			`WITH cur AS (SELECT traffic_used_bytes AS before FROM subscriptions WHERE id = $1)
			 UPDATE subscriptions s
			 SET traffic_used_bytes = LEAST(s.traffic_used_bytes::numeric + $2, 9223372036854775807)::bigint
			 FROM cur, plans p
			 WHERE s.id = $1 AND p.id = s.plan_id
			 RETURNING cur.before, s.traffic_used_bytes, `+effectiveLimit,
			periodID, bytes).Scan(&before, &used, &limit)
		return before, used, limit, err
	}
	var bonus int64
	err = tx.QueryRow(ctx,
		`WITH cur AS (SELECT COALESCE((SELECT used_bytes FROM period_pool_usage WHERE period_id = $1 AND pool_id = $2), 0) AS before)
		 INSERT INTO period_pool_usage (period_id, pool_id, used_bytes) VALUES ($1, $2, $3)
		 ON CONFLICT (period_id, pool_id) DO UPDATE
		 SET used_bytes = LEAST(period_pool_usage.used_bytes::numeric + EXCLUDED.used_bytes, 9223372036854775807)::bigint
		 RETURNING (SELECT before FROM cur), used_bytes, bonus_bytes`,
		periodID, *pool, bytes).Scan(&before, &used, &bonus)
	if err != nil {
		return 0, 0, nil, err
	}
	base, err := c.limitOf(ctx, tx, planID, *pool)
	if err != nil || base == nil {
		return before, used, nil, err
	}
	total := satAdd(*base, bonus)
	return before, used, &total, nil
}

func poolNodes(ctx context.Context, tx pgx.Tx, planID int64, pool *int64) ([]int64, error) {
	rows, err := tx.Query(ctx,
		`SELECT DISTINCT node_id FROM plan_inbounds WHERE plan_id = $1 AND pool_id IS NOT DISTINCT FROM $2`, planID, pool)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
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
