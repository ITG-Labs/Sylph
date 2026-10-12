package model

import (
	"encoding/json"
	"time"
)

type LocationGroup struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Node struct {
	ID             int64      `json:"id"`
	CertCN         string     `json:"cert_cn"`
	Name           string     `json:"name"`
	GroupID        *int64     `json:"group_id"`
	Address        string     `json:"address"`
	UsersVersion   int64      `json:"users_version"`
	Enabled        bool       `json:"enabled"`
	ConfigError    *string    `json:"config_error"`
	LastHeartbeat  *time.Time `json:"last_heartbeat"`
	AgentVersion   *string    `json:"agent_version"`
	XrayVersion    *string    `json:"xray_version"`
	ActiveUsers    *int       `json:"active_users"`
	AgentUptimeSec *int64     `json:"agent_uptime_sec"`
	XrayUptimeSec  *int64     `json:"xray_uptime_sec"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

type NodeHeartbeat struct {
	AgentVersion   string
	XrayVersion    string
	AgentUptimeSec int64
	XrayUptimeSec  *int64
	ActiveUsers    *int
	ConfigError    *string
}

type NodeInbound struct {
	ID        int64           `json:"id"`
	NodeID    int64           `json:"node_id"`
	Tag       string          `json:"tag"`
	Port      int             `json:"port"`
	Protocol  string          `json:"protocol"`
	Transport string          `json:"transport"`
	Security  string          `json:"security"`
	Label     string          `json:"label"`
	Settings  json.RawMessage `json:"-"`
	CreatedAt time.Time       `json:"created_at"`
}

type Plan struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	PriceRub          int64     `json:"price_rub"`
	PeriodDays        int       `json:"period_days"`
	TrafficLimitBytes *int64    `json:"traffic_limit_bytes"`
	LocationGroupID   *int64    `json:"location_group_id"`
	DeviceLimit       *int      `json:"device_limit"`
	CreatedAt         time.Time `json:"created_at"`
}

type User struct {
	ID         int64     `json:"id"`
	TelegramID *int64    `json:"telegram_id"`
	Email      string    `json:"email"`
	SubToken   string    `json:"sub_token"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type UserKey struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	UUID      string    `json:"uuid"`
	CreatedAt time.Time `json:"created_at"`
}

type Subscription struct {
	ID               int64      `json:"id"`
	UserID           int64      `json:"user_id"`
	PlanID           int64      `json:"plan_id"`
	StartsAt         time.Time  `json:"starts_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	Canceled         bool       `json:"canceled"`
	TrafficUsedBytes int64      `json:"traffic_used_bytes"`
	PaidUntil        *time.Time `json:"paid_until,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type Payment struct {
	ID        int64     `json:"id"`
	PaymentID string    `json:"payment_id"`
	UserID    int64     `json:"user_id"`
	PlanID    int64     `json:"plan_id"`
	AmountRub int64     `json:"amount_rub"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type TrafficDelta struct {
	ID         int64     `json:"id"`
	NodeID     int64     `json:"node_id"`
	KeyUUID    string    `json:"key_uuid"`
	BytesUp    int64     `json:"bytes_up"`
	BytesDown  int64     `json:"bytes_down"`
	ReportedAt time.Time `json:"reported_at"`
}

type Device struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	Fingerprint string    `json:"fingerprint"`
	UserAgent   string    `json:"user_agent"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}
