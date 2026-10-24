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
	ErrBanned              = errors.New("user is banned")
)

type User struct {
	ID           int64  `json:"id"`
	TelegramID   int64  `json:"telegram_id"`
	SubToken     string `json:"sub_token"`
	TermsVersion int    `json:"terms_version"`
}

type BotConfig struct {
	Terms   TermsConfig   `json:"terms"`
	Channel ChannelConfig `json:"channel"`
	Help    HelpConfig    `json:"help"`
}

type TermsConfig struct {
	Enabled bool   `json:"enabled"`
	Text    string `json:"text"`
	URL     string `json:"url"`
	Version int    `json:"version"`
}

type ChannelConfig struct {
	Enabled bool   `json:"enabled"`
	Chat    string `json:"chat"`
	URL     string `json:"url"`
	Text    string `json:"text"`
}

type HelpConfig struct {
	Enabled bool         `json:"enabled"`
	Text    string       `json:"text"`
	Buttons []HelpButton `json:"buttons"`
}

type HelpButton struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type Plan struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	PriceRub          int64  `json:"price_rub"`
	PeriodDays        int    `json:"period_days"`
	TrafficLimitBytes *int64 `json:"traffic_limit_bytes"`
	DeviceLimit       *int   `json:"device_limit"`
	OnSale            bool   `json:"on_sale"`
	HWIDRequired      bool   `json:"hwid_required"`
	Trial             bool   `json:"trial"`
	Visibility        string `json:"visibility"`
}

type Period struct {
	PlanID            int64      `json:"plan_id"`
	StartsAt          time.Time  `json:"starts_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	TrafficUsedBytes  int64      `json:"traffic_used_bytes"`
	TrafficLimitBytes *int64     `json:"traffic_limit_bytes"`
	PaidUntil         *time.Time `json:"paid_until"`
}

type Status struct {
	User         User          `json:"user"`
	Subscription *Period       `json:"subscription"`
	Plan         *Plan         `json:"plan"`
	Devices      int           `json:"devices"`
	Trial        *Plan         `json:"trial"`
	DedicatedIPs []DedicatedIP `json:"dedicated_ips"`
}

type DedicatedIP struct {
	IP       string `json:"ip"`
	NodeName string `json:"node_name"`
}

type NodeStats struct {
	NodeID int64  `json:"node_id"`
	Name   string `json:"name"`
	Today  int64  `json:"today"`
	Days7  int64  `json:"days_7"`
	Days30 int64  `json:"days_30"`
	Period int64  `json:"period"`
}

type Stats struct {
	PeriodID *int64      `json:"period_id"`
	Nodes    []NodeStats `json:"nodes"`
}

type Redeem struct {
	Plan    Plan `json:"plan"`
	Already bool `json:"already"`
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

type Device struct {
	ID        int64     `json:"id"`
	UserAgent string    `json:"user_agent"`
	OS        string    `json:"os"`
	Model     string    `json:"model"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

type Core interface {
	Register(ctx context.Context, telegramID int64, profile *Profile) (*User, error)
	Status(ctx context.Context, telegramID int64) (*Status, error)
	Plans(ctx context.Context, userID int64) ([]Plan, error)
	CreatePayment(ctx context.Context, userID, planID int64) (*Payment, error)
	CheckPayment(ctx context.Context, paymentID string) (*Check, error)
	Rotate(ctx context.Context, userID int64) (string, error)
	ResetDevices(ctx context.Context, userID int64) error
	Devices(ctx context.Context, userID int64) ([]Device, error)
	RemoveDevice(ctx context.Context, userID, deviceID int64) error
	GrantTrial(ctx context.Context, userID int64) (time.Time, error)
	Stats(ctx context.Context, userID int64) (*Stats, error)
	RedeemInvite(ctx context.Context, userID int64, code string) (*Redeem, error)
	BotConfig(ctx context.Context) (*BotConfig, error)
	AcceptTerms(ctx context.Context, userID int64, version int) error
}

type CoreClient struct {
	api *apiclient.Client
}

func NewCoreClient(api *apiclient.Client) *CoreClient {
	return &CoreClient{api: api}
}

type Profile struct {
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func (c *CoreClient) Register(ctx context.Context, telegramID int64, profile *Profile) (*User, error) {
	body := struct {
		TelegramID int64    `json:"telegram_id"`
		Profile    *Profile `json:"profile,omitempty"`
	}{telegramID, profile}
	var u User
	return &u, c.do(ctx, http.MethodPost, "/v1/users", body, &u)
}

func (c *CoreClient) Status(ctx context.Context, telegramID int64) (*Status, error) {
	var st Status
	return &st, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/users/%d", telegramID), nil, &st)
}

func (c *CoreClient) Plans(ctx context.Context, userID int64) ([]Plan, error) {
	var plans []Plan
	return plans, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/users/%d/plans", userID), nil, &plans)
}

func (c *CoreClient) GrantTrial(ctx context.Context, userID int64) (time.Time, error) {
	var out struct {
		PaidUntil time.Time `json:"paid_until"`
	}
	err := c.do(ctx, http.MethodPost, fmt.Sprintf("/v1/users/%d/trial", userID), nil, &out)
	return out.PaidUntil, err
}

func (c *CoreClient) Stats(ctx context.Context, userID int64) (*Stats, error) {
	var st Stats
	return &st, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/users/%d/stats", userID), nil, &st)
}

func (c *CoreClient) RedeemInvite(ctx context.Context, userID int64, code string) (*Redeem, error) {
	var r Redeem
	return &r, c.do(ctx, http.MethodPost, "/v1/invites/redeem", map[string]any{"user_id": userID, "code": code}, &r)
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

func (c *CoreClient) Devices(ctx context.Context, userID int64) ([]Device, error) {
	var out []Device
	return out, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/users/%d/devices", userID), nil, &out)
}

func (c *CoreClient) RemoveDevice(ctx context.Context, userID, deviceID int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/v1/users/%d/devices/%d", userID, deviceID), nil, nil)
}

func (c *CoreClient) BotConfig(ctx context.Context) (*BotConfig, error) {
	var cfg BotConfig
	return &cfg, c.do(ctx, http.MethodGet, "/v1/bot/config", nil, &cfg)
}

func (c *CoreClient) AcceptTerms(ctx context.Context, userID int64, version int) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/v1/users/%d/terms", userID), map[string]int{"version": version}, nil)
}

func (c *CoreClient) do(ctx context.Context, method, path string, in, out any) error {
	err := c.api.Do(ctx, method, path, in, out)
	var apiErr *apiclient.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.StatusCode == http.StatusForbidden && apiErr.Code == "banned":
		return fmt.Errorf("%w: %v", ErrBanned, err)
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
