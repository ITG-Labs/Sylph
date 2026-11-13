package clientroute

import (
	"slices"
	"strings"
	"time"
)

const (
	ActionDirect = "direct"
	ActionProxy  = "proxy"
	ActionBlock  = "block"
	ActionNode   = "node"

	MaxRules  = 64
	MaxValues = 1000
	MaxBytes  = 16384
	MaxName   = 64
)

type Rule struct {
	Name    string   `json:"name"`
	Action  string   `json:"action"`
	NodeID  int64    `json:"node_id,omitempty"`
	Domain  []string `json:"domain,omitempty"`
	IP      []string `json:"ip,omitempty"`
	Port    string   `json:"port,omitempty"`
	Network string   `json:"network,omitempty"`
}

type Tags struct {
	Release   string    `json:"release"`
	Geosite   []string  `json:"geosite"`
	Geoip     []string  `json:"geoip"`
	FetchedAt time.Time `json:"fetched_at"`
}

func (t *Tags) Loaded() bool {
	return t != nil && len(t.Geosite) > 0 && len(t.Geoip) > 0
}

func (t *Tags) Missing(r Rule) []string {
	if !t.Loaded() {
		return nil
	}
	var out []string
	for _, v := range r.Domain {
		if code, ok := strings.CutPrefix(v, "geosite:"); ok && !slices.Contains(t.Geosite, code) {
			out = append(out, v)
		}
	}
	for _, v := range r.IP {
		if code, ok := strings.CutPrefix(v, "geoip:"); ok && !slices.Contains(t.Geoip, code) {
			out = append(out, v)
		}
	}
	return out
}

func category(v string) bool {
	return strings.HasPrefix(v, "geosite:") || strings.HasPrefix(v, "geoip:")
}
