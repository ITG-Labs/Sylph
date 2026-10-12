package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/ITG-Labs/sylph/pkg/apiclient"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("conflict")
	ErrPaymentsDisabled    = errors.New("payments disabled")
	ErrProviderUnavailable = errors.New("payment provider unavailable")
)

type User struct {
	ID       int64  `json:"id"`
	SubToken string `json:"sub_token"`
}

type Plan struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	PriceRub          int64  `json:"price_rub"`
	PeriodDays        int    `json:"period_days"`
	TrafficLimitBytes *int64 `json:"traffic_limit_bytes"`
	DeviceLimit       *int   `json:"device_limit"`
	OnSale            bool   `json:"on_sale"`
}

type Period struct {
	PlanID           int64      `json:"plan_id"`
	StartsAt         time.Time  `json:"starts_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	TrafficUsedBytes int64      `json:"traffic_used_bytes"`
	PaidUntil        *time.Time `json:"paid_until"`
}

type Status struct {
	User         User    `json:"user"`
	Subscription *Period `json:"subscription"`
	Plan         *Plan   `json:"plan"`
	Devices      int     `json:"devices"`
}

type Payment struct {
	PaymentID       string `json:"payment_id"`
	ConfirmationURL string `json:"confirmation_url"`
	AmountRub       int64  `json:"amount_rub"`
}

type Check struct {
	Status     string     `json:"status"`
	Applied    bool       `json:"applied"`
	TelegramID *int64     `json:"telegram_id"`
	PaidUntil  *time.Time `json:"paid_until"`
}

type Core interface {
	Register(ctx context.Context, telegramID int64) (*User, error)
	Status(ctx context.Context, telegramID int64) (*Status, error)
	Plans(ctx context.Context) ([]Plan, error)
	CreatePayment(ctx context.Context, userID, planID int64) (*Payment, error)
	CheckPayment(ctx context.Context, paymentID string) (*Check, error)
	Rotate(ctx context.Context, userID int64) (string, error)
	ResetDevices(ctx context.Context, userID int64) error
}

type CoreClient struct {
	api *apiclient.Client
}

func NewCoreClient(api *apiclient.Client) *CoreClient {
	return &CoreClient{api: api}
}

func (c *CoreClient) Register(ctx context.Context, telegramID int64) (*User, error) {
	var u User
	return &u, c.do(ctx, http.MethodPost, "/v1/users", map[string]int64{"telegram_id": telegramID}, &u)
}

func (c *CoreClient) Status(ctx context.Context, telegramID int64) (*Status, error) {
	var st Status
	return &st, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/users/%d", telegramID), nil, &st)
}

func (c *CoreClient) Plans(ctx context.Context) ([]Plan, error) {
	var plans []Plan
	return plans, c.do(ctx, http.MethodGet, "/v1/plans", nil, &plans)
}

func (c *CoreClient) CreatePayment(ctx context.Context, userID, planID int64) (*Payment, error) {
	var p Payment
	return &p, c.do(ctx, http.MethodPost, "/v1/payments/create", map[string]int64{"user_id": userID, "plan_id": planID}, &p)
}

func (c *CoreClient) CheckPayment(ctx context.Context, paymentID string) (*Check, error) {
	var chk Check
	return &chk, c.do(ctx, http.MethodPost, "/v1/payments/"+url.PathEscape(paymentID)+"/check", nil, &chk)
}

func (c *CoreClient) Rotate(ctx context.Context, userID int64) (string, error) {
	var out struct {
		SubToken string `json:"sub_token"`
	}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/v1/users/%d/rotate", userID), nil, &out); err != nil {
		return "", err
	}
	return out.SubToken, nil
}

func (c *CoreClient) ResetDevices(ctx context.Context, userID int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/v1/users/%d/devices/reset", userID), nil, nil)
}

func (c *CoreClient) do(ctx context.Context, method, path string, in, out any) error {
	err := c.api.Do(ctx, method, path, in, out)
	var apiErr *apiclient.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case apiErr.StatusCode == http.StatusConflict:
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case apiErr.StatusCode == http.StatusServiceUnavailable && apiErr.Code == "payments_disabled":
		return fmt.Errorf("%w: %v", ErrPaymentsDisabled, err)
	case apiErr.StatusCode == http.StatusBadGateway && apiErr.Code == "provider_unavailable":
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	return err
}
