package clientroute

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
)

type itgrayDomain struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type itgrayPort struct {
	Single uint16 `json:"single,omitempty"`
	From   uint16 `json:"from,omitempty"`
	To     uint16 `json:"to,omitempty"`
}

type itgrayConditions struct {
	Domains   []itgrayDomain `json:"domains,omitempty"`
	IPCIDRs   []string       `json:"ip_cidrs,omitempty"`
	Geo       []string       `json:"geo,omitempty"`
	Ports     []itgrayPort   `json:"ports,omitempty"`
	Protocols []string       `json:"protocols,omitempty"`
}

type itgrayRule struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Enabled    bool             `json:"enabled"`
	Action     string           `json:"action"`
	Conditions itgrayConditions `json:"conditions"`
	ServerName string           `json:"server_name,omitempty"`
}

type itgrayGroup struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Enabled bool         `json:"enabled"`
	Rules   []itgrayRule `json:"rules"`
}

type itgrayEnvelope struct {
	V      int           `json:"v"`
	Kind   string        `json:"kind"`
	Name   string        `json:"name"`
	Groups []itgrayGroup `json:"groups"`
}

var itgrayKinds = map[string]string{"domain": "suffix", "full": "exact", "keyword": "keyword", "regexp": "regex"}

func renderITGRay(in Input, rules []Resolved) (string, error) {
	out := make([]itgrayRule, 0, len(rules))
	for _, r := range rules {
		action := r.Action
		if action == ActionNode {
			action = ActionProxy
		}
		ir := itgrayRule{Name: r.Name, Enabled: true, Action: action, ServerName: r.ServerName}
		for _, v := range r.Domain {
			prefix, value, _ := strings.Cut(v, ":")
			if prefix == "geosite" {
				ir.Conditions.Geo = append(ir.Conditions.Geo, v)
				continue
			}
			ir.Conditions.Domains = append(ir.Conditions.Domains, itgrayDomain{Kind: itgrayKinds[prefix], Value: value})
		}
		for _, v := range r.IP {
			if strings.HasPrefix(v, "geoip:") {
				ir.Conditions.Geo = append(ir.Conditions.Geo, v)
				continue
			}
			ir.Conditions.IPCIDRs = append(ir.Conditions.IPCIDRs, v)
		}
		ir.Conditions.Ports = itgrayPorts(r.Port)
		if r.Network != "" {
			ir.Conditions.Protocols = []string{r.Network}
		}
		out = append(out, ir)
	}
	raw, err := json.Marshal(itgrayEnvelope{V: 1, Kind: "rules", Name: in.Title, Groups: []itgrayGroup{{Name: in.Title, Enabled: true, Rules: out}}})
	if err != nil {
		return "", err
	}
	return "itgray://rules/import/" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func itgrayPorts(spec string) []itgrayPort {
	if spec == "" {
		return nil
	}
	var out []itgrayPort
	for part := range strings.SplitSeq(spec, ",") {
		lo, hi, isRange := strings.Cut(strings.TrimSpace(part), "-")
		a, _ := strconv.ParseUint(lo, 10, 16)
		if !isRange {
			out = append(out, itgrayPort{Single: uint16(a)})
			continue
		}
		b, _ := strconv.ParseUint(hi, 10, 16)
		if a == b {
			out = append(out, itgrayPort{Single: uint16(a)})
			continue
		}
		out = append(out, itgrayPort{From: uint16(a), To: uint16(b)})
	}
	return out
}
