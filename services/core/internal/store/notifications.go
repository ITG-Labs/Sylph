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
	LimitBytes   *int64
	PaidUntil    *time.Time
	TailTrial    bool
	EndedAt      *time.Time
	EndedTrial   bool
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
		`WITH cur AS (
		     SELECT DISTINCT ON (s.user_id) s.user_id, s.id, s.starts_at, s.expires_at, s.traffic_used_bytes, `+effectiveLimit+` AS lim
		     FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		     WHERE NOT s.canceled AND s.starts_at <= $1 AND s.expires_at > $1
		     ORDER BY s.user_id, s.starts_at DESC, s.id DESC
		 ), tail AS (
		     SELECT DISTINCT ON (s.user_id) s.user_id, s.expires_at, p.trial
		     FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		     WHERE NOT s.canceled AND s.expires_at > $1
		     ORDER BY s.user_id, s.expires_at DESC, s.id DESC
		 ), ended AS (
		     SELECT DISTINCT ON (s.user_id) s.user_id, s.expires_at, p.trial
		     FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		     WHERE NOT s.canceled AND s.expires_at <= $1 AND s.expires_at > $1 - interval '24 hours'
		     ORDER BY s.user_id, s.expires_at DESC, s.id DESC
		 )
		 SELECT u.id, u.telegram_id, cur.id, cur.starts_at, cur.expires_at, COALESCE(cur.traffic_used_bytes, 0), cur.lim,
		        tail.expires_at, COALESCE(tail.trial, FALSE), ended.expires_at, COALESCE(ended.trial, FALSE)
		 FROM users u
		 LEFT JOIN cur ON cur.user_id = u.id
		 LEFT JOIN tail ON tail.user_id = u.id
		 LEFT JOIN ended ON ended.user_id = u.id
		 WHERE u.telegram_id IS NOT NULL AND u.status = 'active'
		   AND ((cur.lim > 0 AND cur.traffic_used_bytes::numeric * 100 >= cur.lim::numeric * 80)
		     OR (cur.id IS NOT NULL AND tail.expires_at <= $1 + interval '3 days')
		     OR (cur.id IS NULL AND tail.user_id IS NULL AND ended.user_id IS NOT NULL))
		 ORDER BY u.id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotifyCandidate
	for rows.Next() {
		var c NotifyCandidate
		if err := rows.Scan(&c.UserID, &c.TelegramID, &c.PeriodID, &c.PeriodStarts, &c.PeriodEnds, &c.UsedBytes, &c.LimitBytes,
			&c.PaidUntil, &c.TailTrial, &c.EndedAt, &c.EndedTrial); err != nil {
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
		   AND ((n.paid_until IS NOT NULL AND n.paid_until < now() - interval '1 day')
		     OR (n.period_id IS NOT NULL AND NOT EXISTS (
		         SELECT 1 FROM subscriptions s WHERE s.id = n.period_id AND NOT s.canceled AND s.expires_at > now())))`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
