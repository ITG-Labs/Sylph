package store

import (
	"context"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func (s *Store) UpsertServiceHeartbeat(ctx context.Context, hb model.ServiceHeartbeat) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO services (cert_cn, role, version, uptime_sec, cert_not_after, telegram_conflict_at, last_seen)
		 VALUES ($1, $2, $3, $4, $5, CASE WHEN $6::timestamptz IS NULL THEN NULL ELSE LEAST($6::timestamptz, now()) END, now())
		 ON CONFLICT (cert_cn) DO UPDATE SET role = EXCLUDED.role, version = EXCLUDED.version, uptime_sec = EXCLUDED.uptime_sec,
		   cert_not_after = EXCLUDED.cert_not_after, telegram_conflict_at = EXCLUDED.telegram_conflict_at, last_seen = now()`,
		hb.CertCN, hb.Role, hb.Version, hb.UptimeSec, hb.CertNotAfter, hb.TelegramConflictAt)
	return err
}

func (s *Store) ListServices(ctx context.Context) ([]model.ServiceInfo, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT cert_cn, role, version, uptime_sec, cert_not_after, telegram_conflict_at, last_seen, created_at
		 FROM services ORDER BY role, cert_cn`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ServiceInfo{}
	for rows.Next() {
		var si model.ServiceInfo
		if err := rows.Scan(&si.CertCN, &si.Role, &si.Version, &si.UptimeSec, &si.CertNotAfter, &si.TelegramConflictAt, &si.LastSeen, &si.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

func (s *Store) DeleteService(ctx context.Context, cn string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM services WHERE cert_cn = $1`, cn)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkHealthOK(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO health_marks (name, ok_at) VALUES ($1, now())
		 ON CONFLICT (name) DO UPDATE SET ok_at = now(), failing_since = NULL`, name)
	return err
}

func (s *Store) MarkHealthFailed(ctx context.Context, name, errText string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO health_marks (name, failing_since, failed_at, error) VALUES ($1, now(), now(), $2)
		 ON CONFLICT (name) DO UPDATE SET failing_since = COALESCE(health_marks.failing_since, now()), failed_at = now(), error = $2`,
		name, errText)
	return err
}

func (s *Store) HealthMarks(ctx context.Context) (map[string]model.HealthMark, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, ok_at, failing_since, failed_at, coalesce(error, '') FROM health_marks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]model.HealthMark{}
	for rows.Next() {
		var name string
		var m model.HealthMark
		if err := rows.Scan(&name, &m.OKAt, &m.FailingSince, &m.FailedAt, &m.Error); err != nil {
			return nil, err
		}
		out[name] = m
	}
	return out, rows.Err()
}
