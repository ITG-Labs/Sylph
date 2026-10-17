package monitor

import "time"

type Problem struct {
	Key        string     `json:"key"`
	Category   string     `json:"category"`
	Level      int        `json:"level"`
	Text       string     `json:"text"`
	Since      *time.Time `json:"since,omitempty"`
	DelaySec   int        `json:"delay_sec"`
	Suppresses string     `json:"suppresses,omitempty"`
}

type Service struct {
	CertCN       string     `json:"cert_cn"`
	Role         string     `json:"role"`
	Version      string     `json:"version"`
	UptimeSec    int64      `json:"uptime_sec"`
	CertNotAfter *time.Time `json:"cert_not_after"`
	LastSeen     time.Time  `json:"last_seen"`
	Alive        bool       `json:"alive"`
}

type State struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Problems    []Problem      `json:"problems"`
	Nodes       map[string]int `json:"nodes"`
	Services    []Service      `json:"services"`
	Core        struct {
		Version string `json:"version"`
	} `json:"core"`
}

const CoreUnreachableKey = "core:unreachable"
