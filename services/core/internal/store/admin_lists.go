package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

const (
	PeriodCurrent = "current"
	PeriodQueued  = "queued"
	PeriodClosed  = "closed"
)

type PlanRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type UserRow struct {
	ID                int64      `json:"id"`
	TelegramID        *int64     `json:"telegram_id"`
	Status            string     `json:"status"`
	BannedAt          *time.Time `json:"banned_at"`
	CreatedAt         time.Time  `json:"created_at"`
	Plan              *PlanRef   `json:"plan"`
	PaidUntil         *time.Time `json:"paid_until"`
	TrafficUsedBytes  int64      `json:"traffic_used_bytes"`
	TrafficLimitBytes *int64     `json:"traffic_limit_bytes"`
	Devices           int        `json:"devices"`
	TelegramUsername  *string    `json:"telegram_username"`
	TelegramName      *string    `json:"telegram_name"`
}

type UserFilter struct {
	Number       *int64
	Token        string
	Status       string
	Subscription string
	PlanID       *int64
	Username     string
	Text         string
	Before       int64
	Limit        int
}

const currentPeriodJoin = `LEFT JOIN LATERAL (
	SELECT s.* FROM subscriptions s
	WHERE s.user_id = u.id AND NOT s.canceled AND s.starts_at <= now() AND s.expires_at > now()
	ORDER BY s.starts_at DESC, s.id DESC LIMIT 1) cur ON TRUE
	LEFT JOIN plans cp ON cp.id = cur.plan_id`

func (s *Store) ListUsers(ctx context.Context, f UserFilter) ([]UserRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.telegram_id, u.status, u.banned_at, u.created_at, cur.plan_id, cp.name,
		        (SELECT max(q.expires_at) FROM subscriptions q WHERE q.user_id = u.id AND NOT q.canceled AND q.expires_at > now()),
		        coalesce(cur.traffic_used_bytes, 0),
		        CASE WHEN cp.traffic_limit_bytes IS NOT NULL THEN LEAST(cp.traffic_limit_bytes::numeric + cur.traffic_bonus_bytes, 9223372036854775807)::bigint END,
		        (SELECT count(*) FROM devices d WHERE d.user_id = u.id),
		        tp.username, `+telegramNameSQL+`
		 FROM users u `+currentPeriodJoin+`
		 LEFT JOIN telegram_profiles tp ON tp.user_id = u.id
		 WHERE ($1::bigint IS NULL OR u.id = $1 OR u.telegram_id = $1)
		   AND ($2::text = '' OR u.sub_token = $2)
		   AND ($3::text = '' OR u.status = $3)
		   AND ($4::text = '' OR ($4 = 'active' AND cur.id IS NOT NULL) OR ($4 = 'none' AND cur.id IS NULL))
		   AND ($5::bigint IS NULL OR cur.plan_id = $5)
		   AND ($6::bigint = 0 OR u.id < $6)
		   AND ($8::text = '' OR starts_with(lower(tp.username), $8))
		   AND ($9::text = '' OR strpos(tp.search, $9) > 0)
		 ORDER BY u.id DESC LIMIT $7`,
		f.Number, f.Token, f.Status, f.Subscription, f.PlanID, f.Before, f.Limit, strings.ToLower(f.Username), strings.ToLower(f.Text))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserRow{}
	for rows.Next() {
		var r UserRow
		var planID *int64
		var planName *string
		if err := rows.Scan(&r.ID, &r.TelegramID, &r.Status, &r.BannedAt, &r.CreatedAt, &planID, &planName,
			&r.PaidUntil, &r.TrafficUsedBytes, &r.TrafficLimitBytes, &r.Devices, &r.TelegramUsername, &r.TelegramName); err != nil {
			return nil, err
		}
		if planID != nil {
			r.Plan = &PlanRef{ID: *planID, Name: *planName}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type PeriodRow struct {
	model.Subscription
	PlanName  string  `json:"plan_name"`
	State     string  `json:"state"`
	PaymentID *string `json:"payment_id"`
}

type AccessRow struct {
	PlanID    int64      `json:"plan_id"`
	PlanName  string     `json:"plan_name"`
	Source    string     `json:"source"`
	GrantedAt time.Time  `json:"granted_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

type TrialRow struct {
	PlanID    int64     `json:"plan_id"`
	PeriodID  int64     `json:"period_id"`
	GrantedAt time.Time `json:"granted_at"`
}

type PaymentRow struct {
	model.Payment
	TelegramID *int64 `json:"telegram_id"`
	PlanName   string `json:"plan_name"`
}

type UserCard struct {
	User     *model.User    `json:"user"`
	Telegram *TelegramInfo  `json:"telegram"`
	Periods  []PeriodRow    `json:"periods"`
	Devices  []model.Device `json:"devices"`
	Trial    *TrialRow      `json:"trial"`
	Access   []AccessRow    `json:"access"`
	Payments []PaymentRow   `json:"payments"`

	Addresses     []UserAddress `json:"addresses"`
	DedicatedPlan bool          `json:"dedicated_plan"`
}

const closedPeriodsInCard = 10
const paymentsInCard = 20

func (s *Store) UserCard(ctx context.Context, userID int64) (*UserCard, error) {
	u, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	card := &UserCard{User: u}
	if card.Telegram, err = s.telegramInfo(ctx, userID); err != nil {
		return nil, err
	}
	if card.Periods, err = s.userPeriods(ctx, userID); err != nil {
		return nil, err
	}
	if card.Devices, err = s.ListDevices(ctx, userID); err != nil {
		return nil, err
	}
	var tr TrialRow
	err = s.pool.QueryRow(ctx, `SELECT plan_id, period_id, granted_at FROM trials WHERE user_id = $1`, userID).Scan(&tr.PlanID, &tr.PeriodID, &tr.GrantedAt)
	switch {
	case err == nil:
		card.Trial = &tr
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	if card.Access, err = s.userAccess(ctx, userID); err != nil {
		return nil, err
	}
	if card.Payments, err = s.ListPayments(ctx, PaymentFilter{UserID: &userID, Limit: paymentsInCard}); err != nil {
		return nil, err
	}
	if card.Addresses, err = s.UserAddresses(ctx, userID); err != nil {
		return nil, err
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		  WHERE sub.user_id = $1 AND p.dedicated_ip AND NOT sub.canceled AND sub.starts_at <= now() AND sub.expires_at > now())`,
		userID).Scan(&card.DedicatedPlan); err != nil {
		return nil, err
	}
	return card, nil
}

const periodRowColumns = `s.id, s.user_id, s.plan_id, s.starts_at, s.expires_at, s.canceled, s.traffic_used_bytes, s.traffic_bonus_bytes, ` + effectiveLimit + `, s.created_at, p.name, pay.payment_id`

const periodRowFrom = ` FROM subscriptions s JOIN plans p ON p.id = s.plan_id LEFT JOIN payments pay ON pay.period_id = s.id`

func (s *Store) userPeriods(ctx context.Context, userID int64) ([]PeriodRow, error) {
	open, err := s.periodRows(ctx,
		`SELECT `+periodRowColumns+periodRowFrom+` WHERE s.user_id = $1 AND NOT s.canceled AND s.expires_at > now()
		 ORDER BY s.starts_at, s.id`, userID)
	if err != nil {
		return nil, err
	}
	closed, err := s.periodRows(ctx,
		`SELECT `+periodRowColumns+periodRowFrom+` WHERE s.user_id = $1 AND (s.canceled OR s.expires_at <= now())
		 ORDER BY s.expires_at DESC, s.id DESC LIMIT $2`, userID, closedPeriodsInCard)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range open {
		open[i].State = PeriodQueued
		if !open[i].StartsAt.After(now) {
			open[i].State = PeriodCurrent
		}
	}
	for i := range closed {
		closed[i].State = PeriodClosed
	}
	return append(open, closed...), nil
}

func (s *Store) periodRows(ctx context.Context, query string, args ...any) ([]PeriodRow, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PeriodRow{}
	for rows.Next() {
		var r PeriodRow
		if err := rows.Scan(&r.ID, &r.UserID, &r.PlanID, &r.StartsAt, &r.ExpiresAt, &r.Canceled, &r.TrafficUsedBytes, &r.TrafficBonusBytes,
			&r.TrafficLimitBytes, &r.CreatedAt, &r.PlanName, &r.PaymentID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) userAccess(ctx context.Context, userID int64) ([]AccessRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT a.plan_id, p.name, a.source, a.granted_at, a.revoked_at
		 FROM plan_access a JOIN plans p ON p.id = a.plan_id
		 WHERE a.user_id = $1 ORDER BY a.granted_at DESC, a.plan_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessRow{}
	for rows.Next() {
		var r AccessRow
		if err := rows.Scan(&r.PlanID, &r.PlanName, &r.Source, &r.GrantedAt, &r.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type PaymentFilter struct {
	Status      string
	Source      string
	UserID      *int64
	StuckBefore time.Time
	Before      int64
	Limit       int
}

const paymentColumnsP = `pm.id, pm.payment_id, pm.user_id, pm.plan_id, pm.amount_rub, pm.status, pm.source, coalesce(pm.confirmation_url, ''), pm.paid_at, pm.period_id, pm.note, pm.refunded_at, pm.created_at`

func (s *Store) ListPayments(ctx context.Context, f PaymentFilter) ([]PaymentRow, error) {
	var stuck *time.Time
	if !f.StuckBefore.IsZero() {
		stuck = &f.StuckBefore
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+paymentColumnsP+`, u.telegram_id, p.name
		 FROM payments pm JOIN users u ON u.id = pm.user_id JOIN plans p ON p.id = pm.plan_id
		 WHERE ($1::text = '' OR pm.status = $1)
		   AND ($2::text = '' OR pm.source = $2)
		   AND ($3::bigint IS NULL OR pm.user_id = $3)
		   AND ($4::timestamptz IS NULL OR (pm.status = 'pending' AND pm.created_at < $4))
		   AND ($5::bigint = 0 OR pm.id < $5)
		 ORDER BY pm.id DESC LIMIT $6`,
		f.Status, f.Source, f.UserID, stuck, f.Before, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PaymentRow{}
	for rows.Next() {
		var r PaymentRow
		dest := append(paymentDest(&r.Payment), &r.TelegramID, &r.PlanName)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetPaymentRow(ctx context.Context, paymentID string) (*PaymentRow, error) {
	var r PaymentRow
	dest := append(paymentDest(&r.Payment), &r.TelegramID, &r.PlanName)
	err := s.pool.QueryRow(ctx,
		`SELECT `+paymentColumnsP+`, u.telegram_id, p.name
		 FROM payments pm JOIN users u ON u.id = pm.user_id JOIN plans p ON p.id = pm.plan_id
		 WHERE pm.payment_id = $1`, paymentID).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
