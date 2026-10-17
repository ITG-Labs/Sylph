package store

import (
	"context"
	"time"
)

type UsersSummary struct {
	Total  int64 `json:"total"`
	Active int64 `json:"active_subscription"`
	Trial  int64 `json:"trial"`
	Banned int64 `json:"banned"`
	New1d  int64 `json:"new_today"`
	New7d  int64 `json:"new_7d"`
	New30d int64 `json:"new_30d"`
}

type MoneySummary struct {
	Today  int64 `json:"today"`
	Days7  int64 `json:"days_7"`
	Days30 int64 `json:"days_30"`
}

type PlanRevenue struct {
	PlanID int64  `json:"plan_id"`
	Name   string `json:"name"`
	Amount int64  `json:"amount_rub"`
	Count  int64  `json:"count"`
}

type RevenueSummary struct {
	YooKassa    MoneySummary  `json:"yookassa"`
	Manual      MoneySummary  `json:"manual"`
	ByPlan      []PlanRevenue `json:"by_plan_30d"`
	Refunded30d int64         `json:"refunded_30d"`
}

type AttentionCounts struct {
	Mismatch        int64 `json:"mismatch"`
	StuckPending    int64 `json:"stuck_pending"`
	PaidWhileBanned int64 `json:"paid_while_banned"`
}

type OverviewCounts struct {
	Users        UsersSummary
	Revenue      RevenueSummary
	Attention    AttentionCounts
	TrafficToday map[int64]int64
}

const dayBounds = `WITH b AS (SELECT (date_trunc('day', now() AT TIME ZONE $1)) AT TIME ZONE $1 AS d0)`

func (s *Store) OverviewCounts(ctx context.Context, stuckBefore time.Time) (*OverviewCounts, error) {
	out := &OverviewCounts{TrafficToday: map[int64]int64{}, Revenue: RevenueSummary{ByPlan: []PlanRevenue{}}}
	u := &out.Users
	if err := s.pool.QueryRow(ctx, dayBounds+`
		SELECT count(u.id), count(u.id) FILTER (WHERE u.status = 'banned'),
		       count(u.id) FILTER (WHERE u.created_at >= b.d0),
		       count(u.id) FILTER (WHERE u.created_at >= b.d0 - interval '6 days'),
		       count(u.id) FILTER (WHERE u.created_at >= b.d0 - interval '29 days')
		FROM b LEFT JOIN users u ON TRUE`, s.statsTZ).Scan(&u.Total, &u.Banned, &u.New1d, &u.New7d, &u.New30d); err != nil {
		return nil, err
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT count(DISTINCT s.user_id), count(DISTINCT s.user_id) FILTER (WHERE p.trial)
		 FROM subscriptions s JOIN plans p ON p.id = s.plan_id JOIN users u ON u.id = s.user_id
		 WHERE u.status = 'active' AND NOT s.canceled AND s.starts_at <= now() AND s.expires_at > now()`).Scan(&u.Active, &u.Trial); err != nil {
		return nil, err
	}
	r := &out.Revenue
	if err := s.pool.QueryRow(ctx, dayBounds+`
		SELECT coalesce(sum(amount_rub) FILTER (WHERE source = 'yookassa' AND paid_at >= b.d0), 0),
		       coalesce(sum(amount_rub) FILTER (WHERE source = 'yookassa' AND paid_at >= b.d0 - interval '6 days'), 0),
		       coalesce(sum(amount_rub) FILTER (WHERE source = 'yookassa'), 0),
		       coalesce(sum(amount_rub) FILTER (WHERE source = 'manual' AND paid_at >= b.d0), 0),
		       coalesce(sum(amount_rub) FILTER (WHERE source = 'manual' AND paid_at >= b.d0 - interval '6 days'), 0),
		       coalesce(sum(amount_rub) FILTER (WHERE source = 'manual'), 0)
		FROM b LEFT JOIN payments ON status = 'succeeded' AND paid_at >= b.d0 - interval '29 days'`, s.statsTZ).
		Scan(&r.YooKassa.Today, &r.YooKassa.Days7, &r.YooKassa.Days30, &r.Manual.Today, &r.Manual.Days7, &r.Manual.Days30); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, dayBounds+`
		SELECT pm.plan_id, p.name, sum(pm.amount_rub), count(*)
		FROM payments pm JOIN plans p ON p.id = pm.plan_id, b
		WHERE pm.status = 'succeeded' AND pm.source = 'yookassa' AND pm.paid_at >= b.d0 - interval '29 days'
		GROUP BY pm.plan_id, p.name ORDER BY sum(pm.amount_rub) DESC, pm.plan_id`, s.statsTZ)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pr PlanRevenue
		if err := rows.Scan(&pr.PlanID, &pr.Name, &pr.Amount, &pr.Count); err != nil {
			rows.Close()
			return nil, err
		}
		r.ByPlan = append(r.ByPlan, pr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.pool.QueryRow(ctx, dayBounds+`
		SELECT coalesce(sum(amount_rub), 0) FROM payments, b WHERE status = 'refunded' AND refunded_at >= b.d0 - interval '29 days'`,
		s.statsTZ).Scan(&r.Refunded30d); err != nil {
		return nil, err
	}
	a, err := s.AttentionCounts(ctx, stuckBefore)
	if err != nil {
		return nil, err
	}
	out.Attention = *a
	trows, err := s.pool.Query(ctx,
		`SELECT node_id, LEAST(sum(bytes_up::numeric + bytes_down), 9223372036854775807)::bigint
		 FROM traffic_daily WHERE day = (now() AT TIME ZONE $1)::date GROUP BY node_id`, s.statsTZ)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var id, b int64
		if err := trows.Scan(&id, &b); err != nil {
			return nil, err
		}
		out.TrafficToday[id] = b
	}
	return out, trows.Err()
}

func (s *Store) AttentionCounts(ctx context.Context, stuckBefore time.Time) (*AttentionCounts, error) {
	a := &AttentionCounts{}
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE pm.status = 'mismatch'),
		        count(*) FILTER (WHERE pm.status = 'pending' AND pm.source = 'yookassa' AND pm.created_at < $1),
		        count(*) FILTER (WHERE pm.status = 'succeeded' AND u.status = 'banned' AND pm.paid_at >= u.banned_at)
		 FROM payments pm JOIN users u ON u.id = pm.user_id`, stuckBefore).Scan(&a.Mismatch, &a.StuckPending, &a.PaidWhileBanned)
	if err != nil {
		return nil, err
	}
	return a, nil
}
