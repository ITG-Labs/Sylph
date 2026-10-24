package noderoute

import (
	"encoding/json"
	"slices"
)

const (
	KindDirect  = "direct"
	KindBlock   = "block"
	KindProxy   = "proxy"
	KindCascade = "cascade"
	KindRaw     = "raw"

	StrategyRandom     = "random"
	StrategyRoundRobin = "roundRobin"
	StrategyLeastPing  = "leastPing"

	DefaultProbeURL      = "https://www.gstatic.com/generate_204"
	DefaultProbeInterval = "1m"
)

type Profile struct {
	Outbounds []Outbound `json:"outbounds"`
	Balancers []Balancer `json:"balancers"`
	Rules     []Rule     `json:"rules"`
	Default   string     `json:"default"`
	Probe     Probe      `json:"probe"`
}

type Probe struct {
	URL      string `json:"url"`
	Interval string `json:"interval"`
}

type Outbound struct {
	Tag     string          `json:"tag"`
	Kind    string          `json:"kind"`
	Direct  *Direct         `json:"direct,omitempty"`
	Proxy   *Proxy          `json:"proxy,omitempty"`
	Cascade *Cascade        `json:"cascade,omitempty"`
	Raw     json.RawMessage `json:"raw,omitempty"`
}

type Direct struct {
	IP string `json:"ip"`
}

type Proxy struct {
	Protocol    string `json:"protocol"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	UUID        string `json:"uuid,omitempty"`
	Password    string `json:"password,omitempty"`
	Username    string `json:"username,omitempty"`
	Method      string `json:"method,omitempty"`
	Security    string `json:"security,omitempty"`
	Flow        string `json:"flow,omitempty"`
	SNI         string `json:"sni,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	PublicKey   string `json:"public_key,omitempty"`
	ShortID     string `json:"short_id,omitempty"`
	HasSecret   bool   `json:"has_secret,omitempty"`
}

type Cascade struct {
	NodeID int64 `json:"node_id"`
}

type Balancer struct {
	Tag       string   `json:"tag"`
	Strategy  string   `json:"strategy"`
	Outbounds []string `json:"outbounds"`
	Fallback  string   `json:"fallback,omitempty"`
}

type Rule struct {
	Name     string   `json:"name,omitempty"`
	Domain   []string `json:"domain,omitempty"`
	IP       []string `json:"ip,omitempty"`
	Port     string   `json:"port,omitempty"`
	Network  string   `json:"network,omitempty"`
	Protocol []string `json:"protocol,omitempty"`
	Inbound  []string `json:"inbound,omitempty"`
	Plans    []int64  `json:"plans,omitempty"`
	To       string   `json:"to"`
}

func (r Rule) IsPlanRule() bool {
	return len(r.Plans) > 0
}

func (p *Profile) DirectTags() []string {
	var out []string
	for _, o := range p.Outbounds {
		if o.Kind == KindDirect {
			out = append(out, o.Tag)
		}
	}
	return out
}

func (p *Profile) BalancesDirect() bool {
	direct := p.DirectTags()
	risky := map[string]bool{}
	for _, b := range p.Balancers {
		for _, tag := range append(slices.Clone(b.Outbounds), b.Fallback) {
			if slices.Contains(direct, tag) {
				risky[b.Tag] = true
			}
		}
	}
	if risky[p.Default] {
		return true
	}
	for _, r := range p.Rules {
		if risky[r.To] {
			return true
		}
	}
	return false
}

func (p *Profile) ReachesDirect(plan int64) bool {
	direct := p.DirectTags()
	for _, r := range p.Rules {
		if !r.IsPlanRule() && slices.Contains(direct, r.To) {
			return true
		}
	}
	for _, r := range p.Rules {
		if r.IsPlanRule() && slices.Contains(r.Plans, plan) {
			return slices.Contains(direct, r.To)
		}
	}
	return slices.Contains(direct, p.Default)
}

func (p *Profile) ReachesDirectAny() bool {
	direct := p.DirectTags()
	return slices.Contains(direct, p.Default) || slices.ContainsFunc(p.Rules, func(r Rule) bool { return slices.Contains(direct, r.To) })
}
