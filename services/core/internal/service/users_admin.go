package service

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	maxReasonRunes = 256
	maxExtendDays  = 3650
	maxBonusBytes  = 1 << 50
)

func (s *Service) activeUser(ctx context.Context, userID int64) (*model.User, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.Status == model.UserBanned {
		return nil, store.ErrUserBanned
	}
	return u, nil
}

func adminErr(err error) error {
	switch {
	case errors.Is(err, store.ErrUserBanned):
		return &ConflictError{Field: "user_id", Message: "user is banned"}
	case errors.Is(err, store.ErrNoActivePeriod):
		return &ConflictError{Field: "subscription", Message: "user has no active period"}
	case errors.Is(err, store.ErrUnlimitedPlan):
		return &ConflictError{Field: "bytes", Message: "plan of the current period has no traffic limit"}
	case errors.Is(err, store.ErrPeriodClosed):
		return &ConflictError{Field: "id", Message: "period is already closed or over"}
	}
	return err
}

func (s *Service) BanUser(ctx context.Context, userID int64, reason string) (bool, error) {
	if err := validText("reason", reason, maxReasonRunes); err != nil {
		return false, err
	}
	already, err := s.store.BanUser(ctx, userID, reason, time.Now().UTC())
	if err != nil {
		return false, err
	}
	if !already {
		s.log.LogAttrs(ctx, slog.LevelInfo, "user_banned", slog.Int64("user_id", userID))
	}
	return already, nil
}

func (s *Service) UnbanUser(ctx context.Context, userID int64) (bool, error) {
	already, err := s.store.UnbanUser(ctx, userID)
	if err != nil {
		return false, err
	}
	if !already {
		s.log.LogAttrs(ctx, slog.LevelInfo, "user_unbanned", slog.Int64("user_id", userID))
	}
	return already, nil
}

func (s *Service) ExtendSubscription(ctx context.Context, userID int64, days int) (*model.Subscription, error) {
	if days < 1 || days > maxExtendDays {
		return nil, fieldErr("days", "must be 1-3650")
	}
	sub, err := s.store.ExtendPeriod(ctx, userID, days, time.Now().UTC())
	if err != nil {
		return nil, adminErr(err)
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "subscription_extended", slog.Int64("user_id", userID), slog.Int("days", days))
	return sub, nil
}

func (s *Service) AddTraffic(ctx context.Context, userID, bytes int64) (*model.Subscription, error) {
	if bytes < 1 || bytes > maxBonusBytes {
		return nil, fieldErr("bytes", "must be 1 byte to 1 PiB")
	}
	sub, err := s.store.AddTrafficBonus(ctx, userID, bytes, time.Now().UTC())
	if err != nil {
		return nil, adminErr(err)
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "traffic_bonus_added", slog.Int64("user_id", userID), slog.Int64("bytes", bytes))
	return sub, nil
}

func (s *Service) ChangeSubscriptionPlan(ctx context.Context, userID, planID int64) (bool, error) {
	if planID <= 0 {
		return false, fieldErr("plan_id", "must be positive")
	}
	changed, err := s.store.ChangePeriodPlan(ctx, userID, planID, time.Now().UTC())
	if err != nil {
		return false, adminErr(err)
	}
	if changed {
		s.log.LogAttrs(ctx, slog.LevelInfo, "subscription_plan_changed", slog.Int64("user_id", userID), slog.Int64("plan_id", planID))
	}
	return changed, nil
}

func (s *Service) ClosePeriod(ctx context.Context, periodID int64) error {
	if err := s.store.ClosePeriod(ctx, periodID, time.Now().UTC()); err != nil {
		return adminErr(err)
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "period_closed", slog.Int64("period_id", periodID))
	return nil
}

const (
	defaultPageSize = 50
	maxPageSize     = 100
	maxTokenRunes   = 128
)

type UserQuery struct {
	Q            string
	Status       string
	Subscription string
	PlanID       *int64
	Before       int64
	Limit        int
}

type userSearch struct {
	Number   *int64
	Token    string
	Username string
	Text     string
}

var (
	hexTokenPattern      = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
	usernameQueryPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)
)

const maxSearchTextRunes = 64

func linkToken(q string) (string, bool) {
	link := q
	if i := strings.IndexAny(link, "?#"); i >= 0 {
		link = link[:i]
	}
	link = strings.TrimRight(link, "/")
	if i := strings.Index(link, "://"); i >= 0 && !strings.Contains(link[i+3:], "/") {
		return "", false
	}
	token := link[strings.LastIndex(link, "/")+1:]
	if token == "" || utf8.RuneCountInString(token) > maxTokenRunes {
		return "", false
	}
	if hexTokenPattern.MatchString(token) {
		return strings.ToLower(token), true
	}
	return token, strings.Contains(q, "://")
}

func parseUserSearch(q string) (userSearch, error) {
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		return userSearch{}, nil
	case strings.Trim(q, "0123456789") == "":
		n, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			return userSearch{}, fieldErr("q", "number is too large")
		}
		return userSearch{Number: &n}, nil
	case strings.HasPrefix(q, "@"):
		if nick := q[1:]; usernameQueryPattern.MatchString(nick) {
			return userSearch{Username: nick}, nil
		}
		return userSearch{}, fieldErr("q", "must be a Telegram username after @")
	case strings.Contains(q, "/"):
		if token, ok := linkToken(q); ok {
			return userSearch{Token: token}, nil
		}
		if strings.Contains(q, "://") {
			return userSearch{}, fieldErr("q", "must be a Telegram ID, a user id, a subscription link, @username or a name")
		}
	case hexTokenPattern.MatchString(q):
		return userSearch{Token: strings.ToLower(q)}, nil
	}
	if err := validText("q", q, maxSearchTextRunes); err != nil {
		return userSearch{}, err
	}
	return userSearch{Text: q}, nil
}

func (s *Service) ListUsers(ctx context.Context, q UserQuery) ([]store.UserRow, error) {
	search, err := parseUserSearch(q.Q)
	if err != nil {
		return nil, err
	}
	if q.Status != "" && q.Status != model.UserActive && q.Status != model.UserBanned {
		return nil, fieldErr("status", "must be active or banned")
	}
	if q.Subscription != "" && q.Subscription != "active" && q.Subscription != "none" {
		return nil, fieldErr("subscription", "must be active or none")
	}
	if q.Limit == 0 {
		q.Limit = defaultPageSize
	}
	if q.Limit < 1 || q.Limit > maxPageSize {
		return nil, fieldErr("limit", "must be 1-100")
	}
	return s.store.ListUsers(ctx, store.UserFilter{
		Number: search.Number, Token: search.Token, Username: search.Username, Text: search.Text,
		Status: q.Status, Subscription: q.Subscription,
		PlanID: q.PlanID, Before: q.Before, Limit: q.Limit,
	})
}

func (s *Service) UserCard(ctx context.Context, userID int64) (*store.UserCard, error) {
	return s.store.UserCard(ctx, userID)
}
