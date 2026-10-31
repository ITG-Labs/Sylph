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

type InboundStats struct {
	NodeID   int64  `json:"node_id"`
	NodeName string `json:"node_name"`
	Tag      string `json:"tag"`
	Label    string `json:"label"`
	Visible  int    `json:"node_visible_inbounds"`
	Today    int64  `json:"today"`
	Days7    int64  `json:"days_7"`
	Days30   int64  `json:"days_30"`
	Period   int64  `json:"period"`
}

type UserStats struct {
	PeriodID *int64         `json:"period_id"`
	Nodes    []NodeStats    `json:"nodes"`
	Inbounds []InboundStats `json:"inbounds"`
}

func (s *Store) SetStatsTimezone(ctx context.Context, tz string) error {
	if _, err := s.pool.Exec(ctx, `SELECT now() AT TIME ZONE $1`, tz); err != nil {
		return err
	}
	s.statsTZ = tz
	return nil
}

func (s *Store) addDaily(ctx context.Context, tx pgx.Tx, userID, nodeID, periodID int64, tag string, v UpDown) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO traffic_daily (user_id, node_id, day, period_id, inbound_tag, bytes_up, bytes_down)
		 VALUES ($1, $2, (now() AT TIME ZONE $3)::date, $4, $5, $6, $7)
		 ON CONFLICT (user_id, day, node_id, period_id, inbound_tag) DO UPDATE SET
		   bytes_up   = LEAST(traffic_daily.bytes_up::numeric + EXCLUDED.bytes_up, 9223372036854775807)::bigint,
		   bytes_down = LEAST(traffic_daily.bytes_down::numeric + EXCLUDED.bytes_down, 9223372036854775807)::bigint`,
		userID, nodeID, s.statsTZ, periodID, tag, v.Up, v.Down)
	return err
}

func (s *Store) UserStats(ctx context.Context, userID int64) (UserStats, error) {
	out := UserStats{Nodes: []NodeStats{}, Inbounds: []InboundStats{}}
	var planID int64
	err := s.pool.QueryRow(ctx,
		`SELECT id, plan_id FROM subscriptions WHERE user_id = $1 AND NOT canceled AND starts_at <= now() AND expires_at > now()
		 ORDER BY starts_at DESC, id DESC LIMIT 1`, userID).Scan(&out.PeriodID, &planID)
	if err != nil && err != pgx.ErrNoRows {
		return out, err
	}
	var period int64
	if out.PeriodID != nil {
		period = *out.PeriodID
	}
	rows, err := s.pool.Query(ctx,
		`WITH today AS (SELECT (now() AT TIME ZONE $2)::date AS d),
		 t AS (SELECT node_id, inbound_tag, day, period_id, bytes_up::numeric + bytes_down AS b FROM traffic_daily, today
		       WHERE user_id = $1 AND (day > today.d - 30 OR (period_id = $3 AND $3 <> 0)))
		 SELECT t.node_id, n.name, t.inbound_tag, COALESCE(i.label, ''),
		   (SELECT count(*) FROM plan_inbounds pi WHERE pi.plan_id = $4 AND pi.node_id = t.node_id)::int,
		   LEAST(COALESCE(sum(t.b) FILTER (WHERE t.day = today.d), 0), 9223372036854775807)::bigint,
		   LEAST(COALESCE(sum(t.b) FILTER (WHERE t.day > today.d - 7), 0), 9223372036854775807)::bigint,
		   LEAST(COALESCE(sum(t.b) FILTER (WHERE t.day > today.d - 30), 0), 9223372036854775807)::bigint,
		   LEAST(COALESCE(sum(t.b) FILTER (WHERE t.period_id = $3 AND $3 <> 0), 0), 9223372036854775807)::bigint
		 FROM t JOIN nodes n ON n.id = t.node_id
		 LEFT JOIN node_inbounds i ON i.node_id = t.node_id AND i.tag = t.inbound_tag, today
		 GROUP BY t.node_id, n.name, t.inbound_tag, i.label ORDER BY t.node_id, t.inbound_tag`, userID, s.statsTZ, period, planID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Inbounds = []InboundStats{}
	for rows.Next() {
		var in InboundStats
		if err := rows.Scan(&in.NodeID, &in.NodeName, &in.Tag, &in.Label, &in.Visible, &in.Today, &in.Days7, &in.Days30, &in.Period); err != nil {
			return out, err
		}
		out.Inbounds = append(out.Inbounds, in)
		if n := len(out.Nodes); n == 0 || out.Nodes[n-1].NodeID != in.NodeID {
			out.Nodes = append(out.Nodes, NodeStats{NodeID: in.NodeID, Name: in.NodeName})
		}
		n := &out.Nodes[len(out.Nodes)-1]
		n.Today, n.Days7, n.Days30, n.Period = satAdd(n.Today, in.Today), satAdd(n.Days7, in.Days7), satAdd(n.Days30, in.Days30), satAdd(n.Period, in.Period)
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
