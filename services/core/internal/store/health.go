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

const (
	IncidentMismatch        = "mismatch"
	IncidentPaidWhileBanned = "paid_while_banned"
)

type MoneyIncident struct {
	Kind      string
	PaymentID int64
	UserID    int64
	AmountRub int64
}

func (s *Store) MoneyIncidents(ctx context.Context) ([]MoneyIncident, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT CASE WHEN pm.status = 'mismatch' THEN 'mismatch' ELSE 'paid_while_banned' END, pm.id, pm.user_id, pm.amount_rub
		 FROM payments pm JOIN users u ON u.id = pm.user_id
		 WHERE pm.status = 'mismatch' OR (pm.status = 'succeeded' AND u.status = 'banned' AND pm.paid_at >= u.banned_at)
		 ORDER BY pm.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MoneyIncident{}
	for rows.Next() {
		var in MoneyIncident
		if err := rows.Scan(&in.Kind, &in.PaymentID, &in.UserID, &in.AmountRub); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

type AddressProblem struct {
	NodeID int64
	IP     string
	Status string
	Error  *string
	Users  int64
}

func (s *Store) AddressProblems(ctx context.Context) ([]AddressProblem, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT na.node_id, na.ip, na.status, na.error, count(*)
		 FROM node_addresses na
		 JOIN address_assignments aa ON aa.address_id = na.id
		 JOIN users u ON u.id = aa.user_id AND u.status = 'active'
		 WHERE na.status <> 'ok'
		 GROUP BY na.id
		 ORDER BY na.node_id, na.ip`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AddressProblem{}
	for rows.Next() {
		var a AddressProblem
		if err := rows.Scan(&a.NodeID, &a.IP, &a.Status, &a.Error, &a.Users); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
