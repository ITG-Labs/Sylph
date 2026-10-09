package store

import (
	"context"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func (s *Store) RecordDeltas(ctx context.Context, nodeID int64, deltas []model.TrafficDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, d := range deltas {
		if _, err := tx.Exec(ctx,
			`INSERT INTO traffic_deltas (node_id, key_uuid, bytes_up, bytes_down) VALUES ($1, $2, $3, $4)`,
			nodeID, d.KeyUUID, d.BytesUp, d.BytesDown); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) SumTrafficSince(ctx context.Context, userID int64, since time.Time) (int64, error) {
	var sum int64
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(bytes_up + bytes_down), 0)
		 FROM traffic_deltas td JOIN user_keys uk ON uk.uuid = td.key_uuid
		 WHERE uk.user_id = $1 AND td.reported_at >= $2`,
		userID, since).Scan(&sum)
	return sum, err
}
