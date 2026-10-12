package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

const DefaultStatsTimezone = "Europe/Moscow"

type NodeStats struct {
	NodeID int64  `json:"node_id"`
	Name   string `json:"name"`
	Today  int64  `json:"today"`
	Days7  int64  `json:"days_7"`
	Days30 int64  `json:"days_30"`
	Period int64  `json:"period"`
}

type UserStats struct {
	PeriodID *int64      `json:"period_id"`
	Nodes    []NodeStats `json:"nodes"`
}

func (s *Store) SetStatsTimezone(ctx context.Context, tz string) error {
	if _, err := s.pool.Exec(ctx, `SELECT now() AT TIME ZONE $1`, tz); err != nil {
		return err
	}
	s.statsTZ = tz
	return nil
}

func (s *Store) addDaily(ctx context.Context, tx pgx.Tx, userID, nodeID, periodID, up, down int64) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO traffic_daily (user_id, node_id, day, period_id, bytes_up, bytes_down)
		 VALUES ($1, $2, (now() AT TIME ZONE $3)::date, $4, $5, $6)
		 ON CONFLICT (user_id, day, node_id, period_id) DO UPDATE SET
		   bytes_up   = LEAST(traffic_daily.bytes_up::numeric + EXCLUDED.bytes_up, 9223372036854775807)::bigint,
		   bytes_down = LEAST(traffic_daily.bytes_down::numeric + EXCLUDED.bytes_down, 9223372036854775807)::bigint`,
		userID, nodeID, s.statsTZ, periodID, up, down)
	return err
}

func (s *Store) UserStats(ctx context.Context, userID int64) (UserStats, error) {
	out := UserStats{Nodes: []NodeStats{}}
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM subscriptions WHERE user_id = $1 AND NOT canceled AND starts_at <= now() AND expires_at > now()
		 ORDER BY starts_at DESC, id DESC LIMIT 1`, userID).Scan(&out.PeriodID)
	if err != nil && err != pgx.ErrNoRows {
		return out, err
	}
	var period int64
	if out.PeriodID != nil {
		period = *out.PeriodID
	}
	rows, err := s.pool.Query(ctx,
		`WITH today AS (SELECT (now() AT TIME ZONE $2)::date AS d),
		 t AS (SELECT node_id, day, period_id, bytes_up::numeric + bytes_down AS b FROM traffic_daily, today
		       WHERE user_id = $1 AND (day > today.d - 30 OR (period_id = $3 AND $3 <> 0)))
		 SELECT t.node_id, n.name,
		   LEAST(COALESCE(sum(t.b) FILTER (WHERE t.day = today.d), 0), 9223372036854775807)::bigint,
		   LEAST(COALESCE(sum(t.b) FILTER (WHERE t.day > today.d - 7), 0), 9223372036854775807)::bigint,
		   LEAST(COALESCE(sum(t.b) FILTER (WHERE t.day > today.d - 30), 0), 9223372036854775807)::bigint,
		   LEAST(COALESCE(sum(t.b) FILTER (WHERE t.period_id = $3 AND $3 <> 0), 0), 9223372036854775807)::bigint
		 FROM t JOIN nodes n ON n.id = t.node_id, today
		 GROUP BY t.node_id, n.name ORDER BY t.node_id`, userID, s.statsTZ, period)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var n NodeStats
		if err := rows.Scan(&n.NodeID, &n.Name, &n.Today, &n.Days7, &n.Days30, &n.Period); err != nil {
			return out, err
		}
		out.Nodes = append(out.Nodes, n)
	}
	return out, rows.Err()
}

func (s *Store) CleanupDailyStats(ctx context.Context, olderThanDays int) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM traffic_daily WHERE day <= (now() AT TIME ZONE $1)::date - $2::int`, s.statsTZ, olderThanDays)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
