package store

import (
	"context"
	"encoding/json"
	"time"
)

type AuditEntry struct {
	ID       int64           `json:"id"`
	At       time.Time       `json:"at"`
	PeerCN   string          `json:"peer_cn"`
	Operator string          `json:"operator"`
	Method   string          `json:"method"`
	Route    string          `json:"route"`
	Path     string          `json:"path"`
	Entity   string          `json:"entity"`
	EntityID string          `json:"entity_id"`
	Action   string          `json:"action"`
	Details  json.RawMessage `json:"details"`
}

type AuditFilter struct {
	Entity   string
	EntityID string
	Before   int64
	Limit    int
}

func (s *Store) InsertAudit(ctx context.Context, e AuditEntry) error {
	var details any
	if len(e.Details) > 0 {
		details = []byte(e.Details)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO admin_audit (peer_cn, operator, method, route, path, entity, entity_id, action, details)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`,
		e.PeerCN, e.Operator, e.Method, e.Route, e.Path, e.Entity, e.EntityID, e.Action, details)
	return err
}

func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, at, peer_cn, operator, method, route, path, entity, entity_id, action, details
		 FROM admin_audit
		 WHERE ($1::text = '' OR entity = $1) AND ($2::text = '' OR entity_id = $2) AND ($3::bigint = 0 OR id < $3)
		 ORDER BY id DESC LIMIT $4`, f.Entity, f.EntityID, f.Before, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var details []byte
		if err := rows.Scan(&e.ID, &e.At, &e.PeerCN, &e.Operator, &e.Method, &e.Route, &e.Path, &e.Entity, &e.EntityID, &e.Action, &details); err != nil {
			return nil, err
		}
		if details != nil {
			e.Details = details
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
