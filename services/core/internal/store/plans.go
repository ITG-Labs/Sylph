package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func (s *Store) CreateLocationGroup(ctx context.Context, name string) (*model.LocationGroup, error) {
	g := &model.LocationGroup{Name: name}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO location_groups (name) VALUES ($1) RETURNING id, created_at`,
		name,
	).Scan(&g.ID, &g.CreatedAt)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	return g, nil
}

func (s *Store) ListLocationGroups(ctx context.Context) ([]model.LocationGroup, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, created_at FROM location_groups ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []model.LocationGroup
	for rows.Next() {
		var g model.LocationGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.CreatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

const planColumns = `id, name, price_rub, period_days, traffic_limit_bytes, location_group_id, device_limit, on_sale, created_at`

func (s *Store) scanPlan(row pgxRow) (*model.Plan, error) {
	p := &model.Plan{}
	err := row.Scan(&p.ID, &p.Name, &p.PriceRub, &p.PeriodDays, &p.TrafficLimitBytes, &p.LocationGroupID, &p.DeviceLimit, &p.OnSale, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) CreatePlan(ctx context.Context, p *model.Plan) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (name, price_rub, period_days, traffic_limit_bytes, location_group_id, device_limit)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, on_sale, created_at`,
		p.Name, p.PriceRub, p.PeriodDays, p.TrafficLimitBytes, p.LocationGroupID, p.DeviceLimit,
	).Scan(&p.ID, &p.OnSale, &p.CreatedAt)
	return mapWriteErr(err)
}

func (s *Store) UpdatePlan(ctx context.Context, p *model.Plan) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE plans SET name = $2, price_rub = $3, period_days = $4, traffic_limit_bytes = $5,
		 location_group_id = $6, device_limit = $7, on_sale = $8 WHERE id = $1`,
		p.ID, p.Name, p.PriceRub, p.PeriodDays, p.TrafficLimitBytes, p.LocationGroupID, p.DeviceLimit, p.OnSale)
	if err != nil {
		return mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetPlan(ctx context.Context, id int64) (*model.Plan, error) {
	return s.scanPlan(s.pool.QueryRow(ctx,
		`SELECT `+planColumns+` FROM plans WHERE id = $1`, id))
}

func (s *Store) ListPlans(ctx context.Context) ([]model.Plan, error) {
	return s.listPlans(ctx, `SELECT `+planColumns+` FROM plans ORDER BY id`)
}

func (s *Store) ListPlansOnSale(ctx context.Context) ([]model.Plan, error) {
	return s.listPlans(ctx, `SELECT `+planColumns+` FROM plans WHERE on_sale ORDER BY price_rub, id`)
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
