package store

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"math"
	"slices"
	"time"

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
	return s.RecordDeltaBatchFrom(ctx, nodeID, batchID, time.Time{}, deltas)
}

func (s *Store) RecordDeltaBatchFrom(ctx context.Context, nodeID int64, batchID string, from time.Time, deltas []model.TrafficDelta) (BatchResult, error) {
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
	var spent *time.Time
	if !from.IsZero() {
		spent = &from
	}
	var at time.Time
	if err := tx.QueryRow(ctx,
		`SELECT LEAST(GREATEST(COALESCE($1::timestamptz, now()), now() - interval '366 days'), now())`, spent).Scan(&at); err != nil {
		return BatchResult{}, err
	}
	owners, err := keyOwners(ctx, tx, deltas)
	if err != nil {
		return BatchResult{}, err
	}
	tags, err := nodeTags(ctx, tx, nodeID)
	if err != nil {
		return BatchResult{}, err
	}
	totals := map[int64]*userTotal{}
	keys := make([]string, 0, len(deltas))
	owned := make([]*int64, 0, len(deltas))
	ups := make([]int64, 0, len(deltas))
	downs := make([]int64, 0, len(deltas))
	for _, d := range deltas {
		owner, ok := owners[d.KeyUUID]
		var userID *int64
		if ok {
			userID = &owner
			t := totals[owner]
			if t == nil {
				t = &userTotal{byInbound: map[string]int64{}, daily: map[string]UpDown{}}
				totals[owner] = t
			}
			t.byInbound[d.Inbound] = satAdd(t.byInbound[d.Inbound], satAdd(d.BytesUp, d.BytesDown))
			tag := d.Inbound
			if !tags[tag] {
				tag = ""
			}
			t.daily[tag] = t.daily[tag].plus(UpDown{Up: d.BytesUp, Down: d.BytesDown})
		}
		keys, owned, ups, downs = append(keys, d.KeyUUID), append(owned, userID), append(ups, d.BytesUp), append(downs, d.BytesDown)
	}
	if len(keys) > 0 {
		if _, err := tx.Exec(ctx,
			`INSERT INTO traffic_deltas (node_id, key_uuid, user_id, bytes_up, bytes_down)
			 SELECT $1, d.k, d.u, d.up, d.down FROM unnest($2::text[], $3::bigint[], $4::bigint[], $5::bigint[]) AS d(k, u, up, down)`,
			nodeID, keys, owned, ups, downs); err != nil {
			return BatchResult{}, err
		}
	}
	users := make([]int64, 0, len(totals))
	for id := range totals {
		users = append(users, id)
	}
	slices.Sort(users)
	periods, err := lockActivePeriods(ctx, tx, users, at)
	if err != nil {
		return BatchResult{}, err
	}
	for _, id := range users {
		if _, ok := periods[id]; ok {
			continue
		}
		periodID, planID, err := lockActivePeriod(ctx, tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			periodID, planID, err = lockActivePeriod(ctx, tx, id)
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return BatchResult{}, err
		}
		periods[id] = activePeriod{id: periodID, plan: planID}
	}
	var daily dailyRows
	for _, id := range users {
		t := totals[id]
		for _, tag := range slices.Sorted(maps.Keys(t.daily)) {
			daily.add(id, periods[id].id, tag, t.daily[tag])
		}
	}
	if err := s.addDailyRows(ctx, tx, nodeID, daily, at); err != nil {
		return BatchResult{}, err
	}
	pools := &poolCache{node: nodeID, byTag: map[planTag]*int64{}, limits: map[int64]map[int64]*int64{}}
	type userPools struct {
		id      int64
		period  activePeriod
		entries []poolTotal
	}
	var plan []userPools
	var adds poolAdds
	for _, id := range users {
		p := periods[id]
		if p.id == 0 {
			continue
		}
		entries, err := pools.totals(ctx, tx, p.plan, totals[id].byInbound)
		if err != nil {
			return BatchResult{}, err
		}
		plan = append(plan, userPools{id: id, period: p, entries: entries})
		for _, pt := range entries {
			adds.add(p.id, pt.pool, pt.bytes)
		}
	}
	applied, err := adds.apply(ctx, tx)
	if err != nil {
		return BatchResult{}, err
	}
	var res BatchResult
	var resync []int64
	for _, u := range plan {
		for _, pt := range u.entries {
			r := applied[poolKey{u.period.id, poolID(pt.pool)}]
			limit := (*int64)(nil)
			if pt.pool != nil {
				base, err := pools.limitOf(ctx, tx, u.period.plan, *pt.pool)
				if err != nil {
					return BatchResult{}, err
				}
				if base != nil {
					total := satAdd(*base, r.bonus)
					limit = &total
				}
			}
			if limit != nil && r.before < *limit && r.used >= *limit {
				res.Exhausted = append(res.Exhausted, Exhaustion{UserID: u.id, PoolID: pt.pool, Used: r.used, Limit: *limit})
				nodes, err := poolNodes(ctx, tx, u.period.plan, pt.pool)
				if err != nil {
					return BatchResult{}, err
				}
				resync = append(resync, nodes...)
			}
		}
	}
	if len(resync) > 0 {
		if err := bumpNodes(ctx, tx, resync); err != nil {
			return BatchResult{}, err
		}
	}
	return res, tx.Commit(ctx)
}

type activePeriod struct {
	id   int64
	plan int64
}

func lockActivePeriods(ctx context.Context, tx pgx.Tx, users []int64, at time.Time) (map[int64]activePeriod, error) {
	out := make(map[int64]activePeriod, len(users))
	if len(users) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx,
		`SELECT s.user_id, s.id, s.plan_id FROM subscriptions s
		 WHERE s.id IN (SELECT DISTINCT ON (user_id) id FROM subscriptions
		                WHERE user_id = ANY($1) AND (NOT canceled OR (NOT closed_manually AND expires_at <= now())) AND starts_at <= $2 AND expires_at > $2
		                ORDER BY user_id, starts_at DESC, id DESC)
		   AND (NOT s.canceled OR (NOT s.closed_manually AND s.expires_at <= now())) AND s.starts_at <= $2 AND s.expires_at > $2
		 ORDER BY s.user_id FOR UPDATE OF s`, users, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user int64
		var p activePeriod
		if err := rows.Scan(&user, &p.id, &p.plan); err != nil {
			return nil, err
		}
		out[user] = p
	}
	return out, rows.Err()
}

func lockActivePeriod(ctx context.Context, tx pgx.Tx, userID int64) (periodID, planID int64, err error) {
	err = tx.QueryRow(ctx,
		`SELECT id, plan_id FROM subscriptions
		 WHERE user_id = $1 AND NOT canceled AND starts_at <= now() AND expires_at > now()
		 ORDER BY starts_at DESC, id DESC LIMIT 1 FOR UPDATE`, userID).Scan(&periodID, &planID)
	return periodID, planID, err
}

type userTotal struct {
	byInbound map[string]int64
	daily     map[string]UpDown
}

func nodeTags(ctx context.Context, tx pgx.Tx, nodeID int64) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT tag FROM node_inbounds WHERE node_id = $1`, nodeID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(list))
	for _, tag := range list {
		out[tag] = true
	}
	return out, nil
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

type poolKey struct {
	period int64
	pool   int64
}

type poolResult struct {
	before int64
	used   int64
	bonus  int64
}

func poolID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

type poolAdds struct {
	seen        map[poolKey]int
	mainPeriods []int64
	mainBytes   []int64
	periods     []int64
	pools       []int64
	bytes       []int64
}

func (a *poolAdds) add(periodID int64, pool *int64, bytes int64) {
	if a.seen == nil {
		a.seen = map[poolKey]int{}
	}
	k := poolKey{periodID, poolID(pool)}
	i, ok := a.seen[k]
	switch {
	case ok && pool == nil:
		a.mainBytes[i] = satAdd(a.mainBytes[i], bytes)
	case ok:
		a.bytes[i] = satAdd(a.bytes[i], bytes)
	case pool == nil:
		a.seen[k] = len(a.mainPeriods)
		a.mainPeriods, a.mainBytes = append(a.mainPeriods, periodID), append(a.mainBytes, bytes)
	default:
		a.seen[k] = len(a.periods)
		a.periods, a.pools, a.bytes = append(a.periods, periodID), append(a.pools, *pool), append(a.bytes, bytes)
	}
}

func (a *poolAdds) apply(ctx context.Context, tx pgx.Tx) (map[poolKey]poolResult, error) {
	out := make(map[poolKey]poolResult, len(a.mainPeriods)+len(a.periods))
	collect := func(rows pgx.Rows, pooled bool) error {
		defer rows.Close()
		for rows.Next() {
			var k poolKey
			var r poolResult
			var err error
			if pooled {
				err = rows.Scan(&k.period, &k.pool, &r.before, &r.used, &r.bonus)
			} else {
				err = rows.Scan(&k.period, &r.before, &r.used)
			}
			if err != nil {
				return err
			}
			out[k] = r
		}
		return rows.Err()
	}
	if len(a.mainPeriods) > 0 {
		rows, err := tx.Query(ctx,
			`WITH v AS (SELECT * FROM unnest($1::bigint[], $2::bigint[]) AS v(id, add)),
			      cur AS (SELECT s.id, s.traffic_used_bytes AS before FROM subscriptions s JOIN v ON v.id = s.id)
			 UPDATE subscriptions s
			 SET traffic_used_bytes = LEAST(s.traffic_used_bytes::numeric + v.add, 9223372036854775807)::bigint
			 FROM v JOIN cur ON cur.id = v.id
			 WHERE s.id = v.id
			 RETURNING s.id, cur.before, s.traffic_used_bytes`,
			a.mainPeriods, a.mainBytes)
		if err != nil {
			return nil, err
		}
		if err := collect(rows, false); err != nil {
			return nil, err
		}
	}
	if len(a.periods) > 0 {
		rows, err := tx.Query(ctx,
			`WITH v AS (SELECT * FROM unnest($1::bigint[], $2::bigint[], $3::bigint[]) AS v(period_id, pool_id, add)),
			      cur AS MATERIALIZED (SELECT v.period_id, v.pool_id, COALESCE(u.used_bytes, 0) AS before
			              FROM v LEFT JOIN period_pool_usage u ON u.period_id = v.period_id AND u.pool_id = v.pool_id),
			      ins AS (INSERT INTO period_pool_usage AS p (period_id, pool_id, used_bytes)
			              SELECT period_id, pool_id, add FROM v ORDER BY period_id, pool_id
			              ON CONFLICT (period_id, pool_id) DO UPDATE
			              SET used_bytes = LEAST(p.used_bytes::numeric + EXCLUDED.used_bytes, 9223372036854775807)::bigint
			              RETURNING p.period_id, p.pool_id, p.used_bytes, p.bonus_bytes)
			 SELECT i.period_id, i.pool_id, c.before, i.used_bytes, i.bonus_bytes FROM ins i JOIN cur c USING (period_id, pool_id)`,
			a.periods, a.pools, a.bytes)
		if err != nil {
			return nil, err
		}
		if err := collect(rows, true); err != nil {
			return nil, err
		}
	}
	return out, nil
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
