package store

import (
	"context"
	"errors"
	"time"
)

type NotifyCandidate struct {
	UserID       int64
	TelegramID   int64
	PeriodID     *int64
	PeriodStarts *time.Time
	PeriodEnds   *time.Time
	UsedBytes    int64
	BonusBytes   int64
	LimitBytes   *int64
	MainIdle     bool
	Pools        []PoolTraffic
	Exhausted    bool
	PaidUntil    *time.Time
	TailTrial    bool
	EndedAt      *time.Time
	EndedTrial   bool
}

type PoolTraffic struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Used  int64  `json:"used"`
	Bonus int64  `json:"bonus"`
	Limit *int64 `json:"limit"`
}

type NotifyMark struct {
	UserID    int64
	Kind      string
	Ref       string
	PeriodID  *int64
	PaidUntil *time.Time
	Outcome   string
}

func (s *Store) NotifyCandidates(ctx context.Context, now time.Time) ([]NotifyCandidate, error) {
	rows, err := s.pool.Query(ctx,
		`WITH rel AS MATERIALIZED (SELECT DISTINCT plan_id, pool FROM plan_inbounds),
		 cur AS (
		     SELECT DISTINCT ON (s.user_id) s.user_id, s.id, s.plan_id, s.starts_at, s.expires_at, s.traffic_used_bytes, s.traffic_bonus_bytes, `+effectiveLimit+` AS lim
		     FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		     WHERE NOT s.canceled AND s.starts_at <= $1 AND s.expires_at > $1
		     ORDER BY s.user_id, s.starts_at DESC, s.id DESC
		 ), used AS MATERIALIZED (
		     SELECT x.period_id, x.pool_id, x.position, x.used_bytes, x.bonus_bytes, x.limit_bytes, pp.node_id, pp.group_id, pp.name,
		            x.limit_bytes IS NOT NULL AND x.used_bytes >= x.limit_bytes AS spent
		     FROM cur
		     JOIN period_pools x ON x.period_id = cur.id
		     JOIN rel ON rel.plan_id = cur.plan_id AND rel.pool = COALESCE(x.pool_id, 0)
		     LEFT JOIN plan_pools pp ON pp.id = x.pool_id
		 ), pools AS (
		     SELECT period_id,
		            COALESCE(json_agg(json_build_object(
		                'id', pool_id,
		                'name', COALESCE(NULLIF(name, ''), (SELECT n.name FROM nodes n WHERE n.id = node_id), (SELECT g.name FROM location_groups g WHERE g.id = group_id)),
		                'used', used_bytes, 'bonus', bonus_bytes, 'limit', limit_bytes) ORDER BY position, pool_id)
		              FILTER (WHERE pool_id IS NOT NULL), '[]') AS list,
		            bool_or(pool_id IS NULL) AS main_used,
		            bool_or(pool_id IS NOT NULL AND limit_bytes > 0 AND used_bytes::numeric * 100 >= limit_bytes::numeric * 80) AS hot,
		            bool_and(spent) AS spent
		     FROM used GROUP BY period_id
		 ), tail AS (
		     SELECT DISTINCT ON (s.user_id) s.user_id, s.expires_at, p.trial
		     FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		     WHERE NOT s.canceled AND s.expires_at > $1
		     ORDER BY s.user_id, s.expires_at DESC, s.id DESC
		 ), ended AS (
		     SELECT DISTINCT ON (s.user_id) s.user_id, s.expires_at, p.trial, s.closed_manually
		     FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		     WHERE s.expires_at <= $1 AND s.expires_at > $1 - interval '24 hours'
		     ORDER BY s.user_id, s.expires_at DESC, s.id DESC
		 )
		 SELECT u.id, u.telegram_id, cur.id, cur.starts_at, cur.expires_at, COALESCE(cur.traffic_used_bytes, 0), COALESCE(cur.traffic_bonus_bytes, 0), cur.lim,
		        COALESCE(NOT pools.main_used, FALSE), COALESCE(pools.list, '[]'),
		        CASE WHEN cur.id IS NULL THEN FALSE
		             WHEN pools.period_id IS NOT NULL THEN pools.spent
		             ELSE cur.lim IS NOT NULL AND cur.traffic_used_bytes >= cur.lim END,
		        tail.expires_at, COALESCE(tail.trial, FALSE), ended.expires_at, COALESCE(ended.trial, FALSE)
		 FROM users u
		 LEFT JOIN cur ON cur.user_id = u.id
		 LEFT JOIN pools ON pools.period_id = cur.id
		 LEFT JOIN tail ON tail.user_id = u.id
		 LEFT JOIN ended ON ended.user_id = u.id
		 WHERE u.telegram_id IS NOT NULL AND u.status = 'active'
		   AND ((cur.lim > 0 AND cur.traffic_used_bytes::numeric * 100 >= cur.lim::numeric * 80)
		     OR COALESCE(pools.hot, FALSE)
		     OR (cur.id IS NOT NULL AND tail.expires_at <= $1 + interval '72 hours')
		     OR (cur.id IS NULL AND tail.user_id IS NULL AND ended.user_id IS NOT NULL AND NOT ended.closed_manually))
		 ORDER BY u.id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotifyCandidate
	for rows.Next() {
		var c NotifyCandidate
		if err := rows.Scan(&c.UserID, &c.TelegramID, &c.PeriodID, &c.PeriodStarts, &c.PeriodEnds, &c.UsedBytes, &c.BonusBytes, &c.LimitBytes,
			&c.MainIdle, &c.Pools, &c.Exhausted, &c.PaidUntil, &c.TailTrial, &c.EndedAt, &c.EndedTrial); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) NotifyMarks(ctx context.Context, userIDs []int64) ([]NotifyMark, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, kind, ref, period_id, paid_until, outcome FROM notifications_sent WHERE user_id = ANY($1)`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotifyMark
	for rows.Next() {
		var m NotifyMark
		if err := rows.Scan(&m.UserID, &m.Kind, &m.Ref, &m.PeriodID, &m.PaidUntil, &m.Outcome); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MarkNotified(ctx context.Context, m NotifyMark) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notifications_sent (user_id, kind, ref, period_id, paid_until, outcome)
		 VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (user_id, kind, ref) DO NOTHING`,
		m.UserID, m.Kind, m.Ref, m.PeriodID, m.PaidUntil, m.Outcome)
	if errors.Is(mapWriteErr(err), ErrInvalidReference) {
		return ErrNotFound
	}
	return err
}

func (s *Store) cleanupNotifications(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM notifications_sent n
		 WHERE n.created_at < $1
		   AND ((n.paid_until IS NOT NULL AND n.paid_until < now() - interval '24 hours')
		     OR (n.period_id IS NOT NULL AND NOT EXISTS (
		         SELECT 1 FROM subscriptions s WHERE s.id = n.period_id AND NOT s.canceled AND s.expires_at > now())))`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
