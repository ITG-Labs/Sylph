package subs

import "time"

const (
	StatusOK              = "ok"
	StatusExpired         = "expired"
	StatusTrafficExceeded = "traffic_exceeded"
	StatusHWIDRequired    = "hwid_required"
	StatusDeviceLimit     = "device_limit"
)

type Connection struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	UUID        string `json:"uuid"`
	Flow        string `json:"flow"`
	Transport   string `json:"transport"`
	Security    string `json:"security"`
	SNI         string `json:"sni"`
	PublicKey   string `json:"public_key"`
	ShortID     string `json:"short_id"`
	Fingerprint string `json:"fingerprint"`
}

type View struct {
	Status            string       `json:"status"`
	TrafficUsedBytes  int64        `json:"traffic_used_bytes"`
	TrafficLimitBytes *int64       `json:"traffic_limit_bytes"`
	ExpiresAt         *time.Time   `json:"expires_at"`
	Connections       []Connection `json:"connections"`
}
