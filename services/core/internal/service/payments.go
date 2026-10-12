package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
	"github.com/ITG-Labs/sylph/services/core/internal/yookassa"
)

var (
	ErrPaymentsDisabled    = errors.New("payments are not configured")
	ErrProviderUnavailable = errors.New("payment provider unavailable")
)

const (
	reuseWindow     = 50 * time.Minute
	pollWindow      = 24 * time.Hour
	pollLimit       = 100
	descriptionMax  = 128
	providerTimeout = 25 * time.Second
)

type PaymentProvider interface {
	CreatePayment(ctx context.Context, req yookassa.CreateRequest, idempotenceKey string) (*yookassa.Payment, error)
	GetPayment(ctx context.Context, id string) (*yookassa.Payment, error)
}

func (s *Service) EnablePayments(p PaymentProvider, returnURL string) {
	s.pay, s.returnURL = p, returnURL
}

type CheckResult struct {
	Status     string     `json:"status"`
	Applied    bool       `json:"applied"`
	TelegramID *int64     `json:"telegram_id"`
	PaidUntil  *time.Time `json:"paid_until"`
}

type PollResult struct {
	Checked   int `json:"checked"`
	Succeeded int `json:"succeeded"`
	Canceled  int `json:"canceled"`
	Mismatch  int `json:"mismatch"`
	Failed    int `json:"failed"`
}

func (s *Service) CreatePayment(ctx context.Context, userID, planID int64) (*model.Payment, error) {
	if s.pay == nil {
		return nil, ErrPaymentsDisabled
	}
	plan, err := s.store.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return nil, err
	}
	if plan.PriceRub <= 0 {
		return nil, &ConflictError{Field: "plan_id", Message: "plan is free"}
	}
	if !plan.OnSale {
		sub, err := s.store.GetActiveSubscription(ctx, userID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if sub == nil || sub.PlanID != plan.ID {
			return nil, &ConflictError{Field: "plan_id", Message: "plan is not on sale"}
		}
	}
	lock := &s.createLocks[uint64(userID)%uint64(len(s.createLocks))]
	lock.Lock()
	defer lock.Unlock()
	existing, err := s.store.FindPendingPayment(ctx, userID, planID, plan.PriceRub, time.Now().Add(-reuseWindow))
	switch {
	case err == nil:
		status, _, err := s.reconcile(ctx, existing, time.Time{})
		if err != nil || status == "pending" {
			return existing, nil
		}
	case !errors.Is(err, store.ErrNotFound):
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	remote, err := s.pay.CreatePayment(pctx, yookassa.CreateRequest{
		Amount:       yookassa.RUB(plan.PriceRub),
		Capture:      true,
		Confirmation: yookassa.Confirmation{Type: "redirect", ReturnURL: s.returnURL},
		Description:  truncateRunes(plan.Name, descriptionMax),
		Metadata:     map[string]string{"user_id": strconv.FormatInt(userID, 10), "plan_id": strconv.FormatInt(planID, 10)},
	}, model.NewUUID())
	if err != nil {
		s.log.LogAttrs(ctx, slog.LevelError, "yookassa_request_failed",
			slog.String("op", "create"),
			slog.Int64("user_id", userID),
			slog.String("error", err.Error()))
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	if remote.ID == "" || len(remote.ID) > 60 || remote.Confirmation == nil || remote.Confirmation.ConfirmationURL == "" {
		s.log.LogAttrs(ctx, slog.LevelError, "yookassa_request_failed",
			slog.String("op", "create"),
			slog.String("payment_id", remote.ID),
			slog.String("error", "payment without id or confirmation url"))
		return nil, fmt.Errorf("%w: payment without id or confirmation url", ErrProviderUnavailable)
	}
	p := &model.Payment{PaymentID: remote.ID, UserID: userID, PlanID: planID, AmountRub: plan.PriceRub,
		Status: "pending", Source: "yookassa", ConfirmationURL: remote.Confirmation.ConfirmationURL}
	if err := s.store.CreatePayment(ctx, p); err != nil {
		return nil, err
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "payment_created",
		slog.String("payment_id", p.PaymentID),
		slog.Int64("user_id", userID),
		slog.Int64("plan_id", planID),
		slog.Int64("amount_rub", plan.PriceRub),
		slog.Bool("test", remote.Test))
	return p, nil
}

func (s *Service) CheckPayment(ctx context.Context, paymentID string) (*CheckResult, error) {
	if s.pay == nil {
		return nil, ErrPaymentsDisabled
	}
	local, err := s.store.GetPayment(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	if local.Source != "yookassa" {
		return nil, store.ErrNotFound
	}
	status, applied := local.Status, false
	if status == "pending" {
		if status, applied, err = s.reconcile(ctx, local, time.Time{}); err != nil {
			return nil, err
		}
	}
	return s.checkResult(ctx, local.UserID, status, applied)
}

func (s *Service) checkResult(ctx context.Context, userID int64, status string, applied bool) (*CheckResult, error) {
	res := &CheckResult{Status: status, Applied: applied}
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	res.TelegramID = u.TelegramID
	sub, err := s.store.GetActiveSubscription(ctx, userID)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return nil, err
	default:
		res.PaidUntil = sub.PaidUntil
	}
	return res, nil
}

func (s *Service) PollPayments(ctx context.Context) (PollResult, error) {
	if s.pay == nil {
		return PollResult{}, ErrPaymentsDisabled
	}
	pending, err := s.store.ListPendingPayments(ctx, time.Now().Add(-pollWindow), pollLimit)
	if err != nil {
		return PollResult{}, err
	}
	var res PollResult
	deadline := time.Now().Add(s.pollBudget)
	for i := range pending {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if !time.Now().Before(deadline) {
			break
		}
		res.Checked++
		status, applied, err := s.reconcile(ctx, &pending[i], deadline)
		switch {
		case err != nil:
			res.Failed++
		case applied:
			res.Succeeded++
		case status == "canceled":
			res.Canceled++
		case status == "mismatch":
			res.Mismatch++
		}
	}
	return res, nil
}

func (s *Service) reconcile(ctx context.Context, local *model.Payment, deadline time.Time) (string, bool, error) {
	if deadline.IsZero() {
		deadline = time.Now().Add(providerTimeout)
	}
	pctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	remote, err := s.pay.GetPayment(pctx, local.PaymentID)
	if err != nil {
		s.log.LogAttrs(ctx, slog.LevelError, "yookassa_request_failed",
			slog.String("op", "get"),
			slog.String("payment_id", local.PaymentID),
			slog.String("error", err.Error()))
		return "", false, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	switch remote.Status {
	case "succeeded":
		if problem := mismatch(local, remote); problem != "" {
			return s.finish(ctx, local, "mismatch", slog.LevelError, "payment_mismatch", slog.String("problem", problem))
		}
		res, err := s.store.SettlePayment(ctx, local.PaymentID, time.Now().UTC())
		if err != nil {
			return "", false, err
		}
		if res.Applied {
			s.logApplied(ctx, res)
		}
		return res.Payment.Status, res.Applied, nil
	case "canceled":
		party, reason := "", ""
		if d := remote.CancellationDetails; d != nil {
			party, reason = d.Party, d.Reason
		}
		return s.finish(ctx, local, "canceled", slog.LevelInfo, "payment_canceled", slog.String("party", party), slog.String("reason", reason))
	default:
		return "pending", false, nil
	}
}

func (s *Service) finish(ctx context.Context, local *model.Payment, status string, level slog.Level, msg string, attrs ...slog.Attr) (string, bool, error) {
	moved, err := s.store.FinishPayment(ctx, local.PaymentID, status)
	if err != nil {
		return "", false, err
	}
	if !moved {
		current, err := s.store.GetPayment(ctx, local.PaymentID)
		if err != nil {
			return "", false, err
		}
		return current.Status, false, nil
	}
	s.log.LogAttrs(ctx, level, msg, append([]slog.Attr{
		slog.String("payment_id", local.PaymentID),
		slog.Int64("user_id", local.UserID),
	}, attrs...)...)
	return status, false, nil
}

func mismatch(local *model.Payment, remote *yookassa.Payment) string {
	if remote.ID != local.PaymentID {
		return "id"
	}
	if remote.Amount.Currency != "RUB" {
		return "currency"
	}
	if kop, ok := remote.Amount.Kopecks(); !ok || kop != local.AmountRub*100 {
		return "amount"
	}
	if remote.Metadata["user_id"] != strconv.FormatInt(local.UserID, 10) {
		return "metadata.user_id"
	}
	if remote.Metadata["plan_id"] != strconv.FormatInt(local.PlanID, 10) {
		return "metadata.plan_id"
	}
	return ""
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
