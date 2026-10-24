package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

const (
	AddressOK      = "ok"
	AddressError   = "error"
	AddressMissing = "missing"
)

const (
	AddressReasonBanned   = "banned"
	AddressReasonNoPeriod = "no_period"
	AddressReasonLocation = "node_not_in_plan"
	AddressReasonPlan     = "plan_without_dedicated_ip"
	AddressReasonError    = "address_error"
	AddressReasonMissing  = "address_missing"
)

var ErrAddressTaken = errors.New("address is assigned to another user")

var ErrUserHasNodeAddress = errors.New("user already has an address on this node")

var ErrAddressPresent = errors.New("address is still reported by its node")

type AddressAssignment struct {
	UserID           int64     `json:"user_id"`
	TelegramID       *int64    `json:"telegram_id"`
	TelegramUsername *string   `json:"telegram_username"`
	TelegramName     *string   `json:"telegram_name"`
	Source           string    `json:"source"`
	Note             string    `json:"note"`
	AssignedAt       time.Time `json:"assigned_at"`
}

type NodeAddress struct {
	ID          int64              `json:"id"`
	NodeID      int64              `json:"node_id"`
	NodeName    string             `json:"node_name"`
	IP          string             `json:"ip"`
	Status      string             `json:"status"`
	Error       *string            `json:"error"`
	FirstSeenAt time.Time          `json:"first_seen_at"`
	LastSeenAt  time.Time          `json:"last_seen_at"`
	Assignment  *AddressAssignment `json:"assignment"`

	BalancerWarning bool `json:"balancer_warning"`
}

type UserAddress struct {
	AddressID  int64     `json:"address_id"`
	NodeID     int64     `json:"node_id"`
	NodeName   string    `json:"node_name"`
	IP         string    `json:"ip"`
	Status     string    `json:"status"`
	Source     string    `json:"source"`
	Note       string    `json:"note"`
	AssignedAt time.Time `json:"assigned_at"`
	Effective  bool      `json:"effective"`
	Reason     string    `json:"reason,omitempty"`
}

func (s *Store) ReportNodeAddresses(ctx context.Context, nodeID int64, reports []model.AddressReport) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ips := make([]string, 0, len(reports))
	for _, r := range reports {
		status, msg := AddressOK, (*string)(nil)
		if !r.OK {
			status = AddressError
			e := r.Error
			msg = &e
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO node_addresses (node_id, ip, status, error) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (node_id, ip) DO UPDATE SET status = EXCLUDED.status, error = EXCLUDED.error, last_seen_at = now()`,
			nodeID, r.IP, status, msg); err != nil {
			return err
		}
		ips = append(ips, r.IP)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE node_addresses SET status = 'missing', error = NULL
		 WHERE node_id = $1 AND status <> 'missing' AND NOT (ip = ANY($2))`,
		nodeID, ips); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const addressSelect = `SELECT na.id, na.node_id, n.name, na.ip, na.status, na.error, na.first_seen_at, na.last_seen_at,
	aa.user_id, u.telegram_id, tp.username, ` + telegramNameSQL + `, aa.source, aa.note, aa.assigned_at
	FROM node_addresses na
	JOIN nodes n ON n.id = na.node_id
	LEFT JOIN address_assignments aa ON aa.address_id = na.id
	LEFT JOIN users u ON u.id = aa.user_id
	LEFT JOIN telegram_profiles tp ON tp.user_id = aa.user_id`

func scanAddress(row pgxRow) (*NodeAddress, error) {
	var a NodeAddress
	var userID *int64
	var source, note *string
	var assignedAt *time.Time
	var as AddressAssignment
	err := row.Scan(&a.ID, &a.NodeID, &a.NodeName, &a.IP, &a.Status, &a.Error, &a.FirstSeenAt, &a.LastSeenAt,
		&userID, &as.TelegramID, &as.TelegramUsername, &as.TelegramName, &source, &note, &assignedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if userID != nil {
		as.UserID, as.Source, as.Note, as.AssignedAt = *userID, *source, *note, *assignedAt
		a.Assignment = &as
	}
	return &a, nil
}

func (s *Store) ListAddresses(ctx context.Context) ([]NodeAddress, error) {
	rows, err := s.pool.Query(ctx, addressSelect+` ORDER BY n.name, na.node_id, na.ip`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NodeAddress{}
	for rows.Next() {
		a, err := scanAddress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) GetAddress(ctx context.Context, id int64) (*NodeAddress, error) {
	return scanAddress(s.pool.QueryRow(ctx, addressSelect+` WHERE na.id = $1`, id))
}

func (s *Store) AssignAddress(ctx context.Context, addressID, userID int64, note string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var nodeID int64
	err = tx.QueryRow(ctx, `SELECT node_id FROM node_addresses WHERE id = $1 FOR UPDATE`, addressID).Scan(&nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var holder int64
	err = tx.QueryRow(ctx, `SELECT user_id FROM address_assignments WHERE address_id = $1`, addressID).Scan(&holder)
	switch {
	case err == nil && holder != userID:
		return ErrAddressTaken
	case err == nil:
		if _, err := tx.Exec(ctx, `UPDATE address_assignments SET note = $2 WHERE address_id = $1`, addressID, note); err != nil {
			return err
		}
		return tx.Commit(ctx)
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO address_assignments (address_id, node_id, user_id, note) VALUES ($1, $2, $3, $4)`,
		addressID, nodeID, userID, note)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrUserHasNodeAddress
	}
	if err != nil {
		return mapWriteErr(err)
	}
	return tx.Commit(ctx)
}

func (s *Store) UnassignAddress(ctx context.Context, addressID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM address_assignments WHERE address_id = $1`, addressID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAddress(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM node_addresses WHERE id = $1 FOR UPDATE`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != AddressMissing {
		return ErrAddressPresent
	}
	if _, err := tx.Exec(ctx, `DELETE FROM node_addresses WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UserAddresses(ctx context.Context, userID int64) ([]UserAddress, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT na.id, na.node_id, n.name, na.ip, na.status, aa.source, aa.note, aa.assigned_at, u.status,
		        EXISTS (SELECT 1 FROM subscriptions sub
		                WHERE sub.user_id = aa.user_id AND NOT sub.canceled AND sub.starts_at <= now() AND sub.expires_at > now()),
		        cur.dedicated_ip
		 FROM address_assignments aa
		 JOIN node_addresses na ON na.id = aa.address_id
		 JOIN nodes n ON n.id = na.node_id
		 JOIN users u ON u.id = aa.user_id
		 LEFT JOIN LATERAL (
		   SELECT p.dedicated_ip FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		   WHERE sub.user_id = aa.user_id AND NOT sub.canceled AND sub.starts_at <= now() AND sub.expires_at > now()
		     AND (n.group_id IS NULL OR p.location_group_id = n.group_id)
		   ORDER BY sub.starts_at DESC, sub.id DESC LIMIT 1) cur ON TRUE
		 WHERE aa.user_id = $1
		 ORDER BY n.name, na.ip`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserAddress{}
	for rows.Next() {
		var a UserAddress
		var userStatus string
		var anyPeriod bool
		var dedicated *bool
		if err := rows.Scan(&a.AddressID, &a.NodeID, &a.NodeName, &a.IP, &a.Status, &a.Source, &a.Note, &a.AssignedAt,
			&userStatus, &anyPeriod, &dedicated); err != nil {
			return nil, err
		}
		switch {
		case userStatus != "active":
			a.Reason = AddressReasonBanned
		case !anyPeriod:
			a.Reason = AddressReasonNoPeriod
		case dedicated == nil:
			a.Reason = AddressReasonLocation
		case !*dedicated:
			a.Reason = AddressReasonPlan
		case a.Status == AddressError:
			a.Reason = AddressReasonError
		case a.Status == AddressMissing:
			a.Reason = AddressReasonMissing
		default:
			a.Effective = true
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
