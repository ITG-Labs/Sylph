package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func poolsOf(plan string) string {
	return `COALESCE((SELECT json_agg(json_build_object('id', pp.id, 'name', pp.name, 'traffic_limit_bytes', pp.traffic_limit_bytes,
		'inbound_ids', ARRAY(SELECT ppi.inbound_id FROM plan_pool_inbounds ppi WHERE ppi.pool_id = pp.id ORDER BY ppi.inbound_id))
		ORDER BY pp.position, pp.id) FROM plan_pools pp WHERE pp.plan_id = ` + plan + `.id), '[]')`
}

func freeInbounds(plan string) string {
	return `(SELECT count(*) FROM plan_inbounds pi WHERE pi.plan_id = ` + plan + `.id AND pi.pool_id IS NULL)`
}

var planColumns = `id, name, price_rub, period_days, location_group_id, device_limit, on_sale, hwid_required, trial, visibility, dedicated_ip, created_at, ` + poolsOf("plans") + `, ` + freeInbounds("plans")

var planColumnsP = `p.id, p.name, p.price_rub, p.period_days, p.location_group_id, p.device_limit, p.on_sale, p.hwid_required, p.trial, p.visibility, p.dedicated_ip, p.created_at, ` + poolsOf("p") + `, ` + freeInbounds("p")

func (s *Store) scanPlan(row pgxRow) (*model.Plan, error) {
	p := &model.Plan{}
	err := row.Scan(&p.ID, &p.Name, &p.PriceRub, &p.PeriodDays, &p.LocationGroupID, &p.DeviceLimit, &p.OnSale, &p.HWIDRequired, &p.Trial, &p.Visibility, &p.DedicatedIP, &p.CreatedAt, &p.Pools, &p.FreeInbounds)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) CreatePlan(ctx context.Context, p *model.Plan) error {
	return s.CreatePlanWith(ctx, p, true)
}

func (s *Store) CreatePlanWith(ctx context.Context, p *model.Plan, onSale bool) error {
	if p.Visibility == "" {
		p.Visibility = model.VisibilityPublic
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx,
		`INSERT INTO plans (name, price_rub, period_days, location_group_id, device_limit, hwid_required, trial, visibility, on_sale, dedicated_ip)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, on_sale, created_at`,
		p.Name, p.PriceRub, p.PeriodDays, p.LocationGroupID, p.DeviceLimit, p.HWIDRequired, p.Trial, p.Visibility, onSale, p.DedicatedIP,
	).Scan(&p.ID, &p.OnSale, &p.CreatedAt)
	if err != nil {
		return mapWriteErr(err)
	}
	if err := writePools(ctx, tx, p); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UpdatePlan(ctx context.Context, p *model.Plan) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx,
		`UPDATE plans SET name = $2, price_rub = $3, period_days = $4,
		 location_group_id = $5, device_limit = $6, on_sale = $7, hwid_required = $8, trial = $9, visibility = $10, dedicated_ip = $11 WHERE id = $1`,
		p.ID, p.Name, p.PriceRub, p.PeriodDays, p.LocationGroupID, p.DeviceLimit, p.OnSale, p.HWIDRequired, p.Trial, p.Visibility, p.DedicatedIP)
	if err != nil {
		return mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := writePools(ctx, tx, p); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func writePools(ctx context.Context, tx pgx.Tx, p *model.Plan) error {
	if p.Pools == nil {
		p.Pools = []model.PlanPool{}
	}
	rows, err := tx.Query(ctx, `SELECT id FROM plan_pools WHERE plan_id = $1 FOR NO KEY UPDATE`, p.ID)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	own := map[int64]bool{}
	for _, id := range ids {
		own[id] = true
	}
	kept := []int64{}
	for _, pool := range p.Pools {
		if pool.ID != 0 {
			if !own[pool.ID] {
				return ErrInvalidReference
			}
			kept = append(kept, pool.ID)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM plan_pools WHERE plan_id = $1 AND NOT id = ANY($2)`, p.ID, kept); err != nil {
		return err
	}
	var pools, inbounds []int64
	for i := range p.Pools {
		pool := &p.Pools[i]
		if pool.ID != 0 {
			_, err = tx.Exec(ctx,
				`UPDATE plan_pools SET position = $2, name = $3, traffic_limit_bytes = $4 WHERE id = $1`,
				pool.ID, i, pool.Name, pool.TrafficLimitBytes)
		} else {
			err = tx.QueryRow(ctx,
				`INSERT INTO plan_pools (plan_id, position, name, traffic_limit_bytes) VALUES ($1, $2, $3, $4) RETURNING id`,
				p.ID, i, pool.Name, pool.TrafficLimitBytes).Scan(&pool.ID)
		}
		if err != nil {
			return mapWriteErr(err)
		}
		if pool.InboundIDs == nil {
			pool.InboundIDs = []int64{}
		}
		for _, in := range pool.InboundIDs {
			pools, inbounds = append(pools, pool.ID), append(inbounds, in)
		}
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM plan_pool_inbounds ppi WHERE ppi.plan_id = $1
		 AND NOT EXISTS (SELECT 1 FROM unnest($2::bigint[], $3::bigint[]) AS n(pool_id, inbound_id)
		                 WHERE n.pool_id = ppi.pool_id AND n.inbound_id = ppi.inbound_id)`, p.ID, pools, inbounds); err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO plan_pool_inbounds (plan_id, pool_id, inbound_id)
		 SELECT $1, n.pool_id, n.inbound_id FROM unnest($2::bigint[], $3::bigint[]) AS n(pool_id, inbound_id)
		 ON CONFLICT (pool_id, inbound_id) DO NOTHING`, p.ID, pools, inbounds)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrPoolOverlap
		}
		return mapWriteErr(err)
	}
	return tx.QueryRow(ctx, `SELECT `+freeInbounds("p")+` FROM plans p WHERE p.id = $1`, p.ID).Scan(&p.FreeInbounds)
}

func (s *Store) GetPlan(ctx context.Context, id int64) (*model.Plan, error) {
	return s.scanPlan(s.pool.QueryRow(ctx,
		`SELECT `+planColumns+` FROM plans WHERE id = $1`, id))
}

func (s *Store) ListPlans(ctx context.Context) ([]model.Plan, error) {
	return s.listPlans(ctx, `SELECT `+planColumns+` FROM plans ORDER BY id`)
}

type PlanRow struct {
	model.Plan
	ActiveUsers int `json:"active_users"`
}

func (s *Store) ListAdminPlans(ctx context.Context) ([]PlanRow, error) {
	rows, err := s.pool.Query(ctx,
		`WITH cur AS (
		   SELECT sub.plan_id, count(DISTINCT sub.user_id) AS n
		   FROM subscriptions sub JOIN users u ON u.id = sub.user_id
		   WHERE NOT sub.canceled AND sub.starts_at <= now() AND sub.expires_at > now() AND u.status = 'active'
		   GROUP BY sub.plan_id)
		 SELECT `+planColumnsP+`, COALESCE(cur.n, 0)
		 FROM plans p LEFT JOIN cur ON cur.plan_id = p.id ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanRow{}
	for rows.Next() {
		var r PlanRow
		p := &r.Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.PriceRub, &p.PeriodDays, &p.LocationGroupID, &p.DeviceLimit,
			&p.OnSale, &p.HWIDRequired, &p.Trial, &p.Visibility, &p.DedicatedIP, &p.CreatedAt, &p.Pools, &p.FreeInbounds, &r.ActiveUsers); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListPlansOnSale(ctx context.Context) ([]model.Plan, error) {
	return s.listPlans(ctx, `SELECT `+planColumns+` FROM plans WHERE on_sale AND price_rub > 0 AND visibility = 'public' ORDER BY price_rub, id`)
}

func (s *Store) listPlans(ctx context.Context, query string) ([]model.Plan, error) {
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var plans []model.Plan
	for rows.Next() {
		p, err := s.scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *p)
	}
	return plans, rows.Err()
}
