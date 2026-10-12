package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

const (
	AccessInvite = "invite"
	AccessAdmin  = "admin"
)

var (
	ErrInviteRevoked = errors.New("invite revoked")
	ErrInviteExpired = errors.New("invite expired")
	ErrInviteUsedUp  = errors.New("invite used up")
)

type RedeemResult struct {
	PlanID   int64
	InviteID int64
	Already  bool
}

const inviteColumns = `id, code, plan_id, max_uses, uses, expires_at, revoked_at, created_at`

func scanInvite(row pgxRow) (*model.Invite, error) {
	inv := &model.Invite{}
	err := row.Scan(&inv.ID, &inv.Code, &inv.PlanID, &inv.MaxUses, &inv.Uses, &inv.ExpiresAt, &inv.RevokedAt, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return inv, err
}

func (s *Store) VisiblePlans(ctx context.Context, userID int64) ([]model.Plan, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+planColumnsP+` FROM plans p
		 WHERE p.on_sale AND p.price_rub > 0 AND `+visibleToUser+`
		 ORDER BY p.price_rub, p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := []model.Plan{}
	for rows.Next() {
		p, err := s.scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *p)
	}
	return plans, rows.Err()
}

func (s *Store) HasPlanAccess(ctx context.Context, planID, userID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM plan_access WHERE plan_id = $1 AND user_id = $2)`, planID, userID).Scan(&ok)
	return ok, err
}

func (s *Store) GrantPlanAccess(ctx context.Context, planID, userID int64, source string, inviteID *int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO plan_access (plan_id, user_id, source, invite_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		planID, userID, source, inviteID)
	return mapWriteErr(err)
}

func (s *Store) RevokePlanAccess(ctx context.Context, planID, userID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM plan_access WHERE plan_id = $1 AND user_id = $2`, planID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPlanAccess(ctx context.Context, planID int64) ([]model.PlanAccess, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT a.plan_id, a.user_id, u.telegram_id, a.source, a.invite_id, a.granted_at
		 FROM plan_access a JOIN users u ON u.id = a.user_id
		 WHERE a.plan_id = $1 ORDER BY a.granted_at, a.user_id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PlanAccess{}
	for rows.Next() {
		var a model.PlanAccess
		if err := rows.Scan(&a.PlanID, &a.UserID, &a.TelegramID, &a.Source, &a.InviteID, &a.GrantedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) CreateInvite(ctx context.Context, inv *model.Invite) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO plan_invites (code, plan_id, max_uses, expires_at) VALUES ($1, $2, $3, $4)
		 RETURNING id, uses, created_at`,
		inv.Code, inv.PlanID, inv.MaxUses, inv.ExpiresAt).Scan(&inv.ID, &inv.Uses, &inv.CreatedAt)
	return mapWriteErr(err)
}

func (s *Store) ListInvites(ctx context.Context, planID int64) ([]model.Invite, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+inviteColumns+` FROM plan_invites WHERE plan_id = $1 ORDER BY id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

func (s *Store) RevokeInvite(ctx context.Context, id int64, now time.Time) (*model.Invite, error) {
	return scanInvite(s.pool.QueryRow(ctx,
		`UPDATE plan_invites SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1 RETURNING `+inviteColumns, id, now))
}

func (s *Store) RedeemInvite(ctx context.Context, code string, userID int64, now time.Time) (RedeemResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RedeemResult{}, err
	}
	defer tx.Rollback(ctx)
	inv, err := scanInvite(tx.QueryRow(ctx, `SELECT `+inviteColumns+` FROM plan_invites WHERE code = $1 FOR UPDATE`, code))
	if err != nil {
		return RedeemResult{}, err
	}
	res := RedeemResult{PlanID: inv.PlanID, InviteID: inv.ID}
	var has bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM plan_access WHERE plan_id = $1 AND user_id = $2)`, inv.PlanID, userID).Scan(&has); err != nil {
		return RedeemResult{}, err
	}
	if has {
		res.Already = true
		return res, nil
	}
	switch {
	case inv.RevokedAt != nil:
		return RedeemResult{}, ErrInviteRevoked
	case inv.ExpiresAt != nil && !now.Before(*inv.ExpiresAt):
		return RedeemResult{}, ErrInviteExpired
	case inv.MaxUses != nil && inv.Uses >= *inv.MaxUses:
		return RedeemResult{}, ErrInviteUsedUp
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO plan_access (plan_id, user_id, source, invite_id) VALUES ($1, $2, 'invite', $3)`,
		inv.PlanID, userID, inv.ID); err != nil {
		return RedeemResult{}, mapWriteErr(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE plan_invites SET uses = uses + 1 WHERE id = $1`, inv.ID); err != nil {
		return RedeemResult{}, err
	}
	return res, tx.Commit(ctx)
}
