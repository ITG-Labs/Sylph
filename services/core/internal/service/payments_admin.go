package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
	"github.com/ITG-Labs/sylph/services/core/internal/yookassa"
)

var paymentStatuses = []string{"pending", "succeeded", "canceled", "mismatch", "rejected", "refunded"}

type PaymentQuery struct {
	Status string
	Source string
	UserID *int64
	Stuck  bool
	Before int64
	Limit  int
}

type ProviderView struct {
	Found    bool   `json:"found"`
	Status   string `json:"status,omitempty"`
	Amount   string `json:"amount,omitempty"`
	Refunded string `json:"refunded,omitempty"`
}

type PaymentActionResult struct {
	Status       string        `json:"status"`
	Applied      bool          `json:"applied"`
	PeriodID     *int64        `json:"period_id,omitempty"`
	PeriodClosed bool          `json:"period_closed"`
	Provider     *ProviderView `json:"provider,omitempty"`
}

func validReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fieldErr("reason", "is required")
	}
	return validText("reason", reason, maxReasonRunes)
}

func paymentErr(err error) error {
	var pse *store.PaymentStateError
	if errors.As(err, &pse) {
		return &ConflictError{Field: "status", Message: pse.Error()}
	}
	return adminErr(err)
}

func (s *Service) ListPayments(ctx context.Context, q PaymentQuery) ([]store.PaymentRow, error) {
	if q.Status != "" && !slices.Contains(paymentStatuses, q.Status) {
		return nil, fieldErr("status", "must be one of "+strings.Join(paymentStatuses, ", "))
	}
	if q.Source != "" && q.Source != "manual" && q.Source != "yookassa" {
		return nil, fieldErr("source", "must be manual or yookassa")
	}
	if q.Limit == 0 {
		q.Limit = defaultPageSize
	}
	if q.Limit < 1 || q.Limit > maxPageSize {
		return nil, fieldErr("limit", "must be 1-100")
	}
	f := store.PaymentFilter{Status: q.Status, Source: q.Source, UserID: q.UserID, Before: q.Before, Limit: q.Limit}
	if q.Stuck {
		f.StuckBefore = time.Now().Add(-pollWindow)
	}
	return s.store.ListPayments(ctx, f)
}

func (s *Service) GetPaymentRow(ctx context.Context, paymentID string) (*store.PaymentRow, error) {
	return s.store.GetPaymentRow(ctx, paymentID)
}

func providerView(remote *yookassa.Payment) *ProviderView {
	if remote == nil {
		return &ProviderView{Found: false}
	}
	v := &ProviderView{Found: true, Status: remote.Status, Amount: remote.Amount.Value}
	if remote.RefundedAmount != nil {
		v.Refunded = remote.RefundedAmount.Value
	}
	return v
}

func (s *Service) providerPayment(ctx context.Context, paymentID string) (*model.Payment, *yookassa.Payment, error) {
	if s.pay == nil {
		return nil, nil, ErrPaymentsDisabled
	}
	local, err := s.store.GetPayment(ctx, paymentID)
	if err != nil {
		return nil, nil, err
	}
	if local.Source != "yookassa" {
		return nil, nil, &ConflictError{Field: "source", Message: "only YooKassa payments can be checked with the shop"}
	}
	remote, err := s.fetchRemote(ctx, paymentID, time.Time{})
	if errors.Is(err, yookassa.ErrNotFound) {
		return local, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return local, remote, nil
}

func (s *Service) RecheckPayment(ctx context.Context, paymentID string) (*PaymentActionResult, error) {
	local, remote, err := s.providerPayment(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	res := &PaymentActionResult{Status: local.Status, Provider: providerView(remote)}
	if remote != nil && local.Status == "pending" {
		if res.Status, res.Applied, err = s.settleRemote(ctx, local, remote); err != nil {
			return nil, err
		}
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "payment_rechecked",
		slog.String("payment_id", paymentID),
		slog.String("status", res.Status),
		slog.Bool("found", remote != nil))
	return res, nil
}

func (s *Service) ApplyPayment(ctx context.Context, paymentID, reason string) (*PaymentActionResult, error) {
	if err := validReason(reason); err != nil {
		return nil, err
	}
	res, err := s.store.ApplyMismatchPayment(ctx, paymentID, reason, time.Now().UTC())
	if err != nil {
		return nil, paymentErr(err)
	}
	s.logApplied(ctx, res)
	return &PaymentActionResult{Status: res.Payment.Status, Applied: true, PeriodID: res.Payment.PeriodID}, nil
}

func (s *Service) RejectPayment(ctx context.Context, paymentID, reason string) (*PaymentActionResult, error) {
	if err := validReason(reason); err != nil {
		return nil, err
	}
	local, err := s.store.GetPayment(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	res := &PaymentActionResult{}
	switch {
	case local.Status == "mismatch":
		err = s.store.RejectPayment(ctx, paymentID, reason, "mismatch")
	case local.Status == "pending" && local.Source == "yookassa":
		var remote *yookassa.Payment
		if local, remote, err = s.providerPayment(ctx, paymentID); err != nil {
			return nil, err
		}
		res.Provider = providerView(remote)
		if remote != nil && remote.Status != "pending" && remote.Status != "waiting_for_capture" {
			if res.Status, res.Applied, err = s.settleRemote(ctx, local, remote); err != nil {
				return nil, err
			}
			return res, nil
		}
		if remote != nil && local.CreatedAt.After(time.Now().Add(-pollWindow)) {
			return nil, &ConflictError{Field: "status", Message: "payment is still waiting for the customer"}
		}
		err = s.store.RejectPayment(ctx, paymentID, reason, "pending")
	case local.Status == "pending":
		err = s.store.RejectPayment(ctx, paymentID, reason, "pending")
	default:
		return nil, &ConflictError{Field: "status", Message: "payment is " + local.Status}
	}
	if err != nil {
		return nil, paymentErr(err)
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "payment_rejected", slog.String("payment_id", paymentID), slog.String("was", local.Status))
	res.Status = "rejected"
	return res, nil
}

func (s *Service) RefundPayment(ctx context.Context, paymentID, reason string) (*PaymentActionResult, error) {
	if err := validReason(reason); err != nil {
		return nil, err
	}
	closed, err := s.store.RefundPayment(ctx, paymentID, reason, time.Now().UTC())
	if err != nil {
		return nil, paymentErr(err)
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "payment_refunded", slog.String("payment_id", paymentID), slog.Bool("period_closed", closed))
	return &PaymentActionResult{Status: "refunded", PeriodClosed: closed}, nil
}
