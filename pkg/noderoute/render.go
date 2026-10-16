package noderoute

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

const (
	ServiceOutboundTag = "sylph_direct"
	ServiceRuleTag     = "sylph-service"
	DefaultRuleTag     = "sylph-default"
)

var directStrategies = map[string]string{
	"auto":        "AsIs",
	"ipv4":        "ForceIPv4",
	"ipv6":        "ForceIPv6",
	"prefer_ipv4": "UseIPv4v6",
	"prefer_ipv6": "UseIPv6v4",
}

type RenderInput struct {
	Profile     *Profile
	ServiceKeys []string
	GeoFile     func(source string) string
}

type Static struct {
	Outbounds   []map[string]any
	Routing     map[string]any
	Observatory map[string]any
}

type TailRule struct {
	Tag         string
	Users       []string
	CatchAll    bool
	OutboundTag string
	BalancerTag string
}

func (p *Profile) isBalancer(tag string) bool {
	return slices.ContainsFunc(p.Balancers, func(b Balancer) bool { return b.Tag == tag })
}

func (p *Profile) target(rule map[string]any, tag string) {
	if p.isBalancer(tag) {
		rule["balancerTag"] = tag
	} else {
		rule["outboundTag"] = tag
	}
}

func Render(in RenderInput) (Static, error) {
	p := in.Profile
	if err := ValidateResolved(p); err != nil {
		return Static{}, err
	}
	var out Static
	ordered := slices.Clone(p.Outbounds)
	if i := slices.IndexFunc(ordered, func(o Outbound) bool { return o.Tag == p.Default }); i > 0 {
		first := ordered[i]
		ordered = append([]Outbound{first}, slices.Delete(ordered, i, i+1)...)
	}
	for _, o := range ordered {
		rendered, err := renderOutbound(o)
		if err != nil {
			return Static{}, err
		}
		out.Outbounds = append(out.Outbounds, rendered)
	}
	var rules []any
	if len(in.ServiceKeys) > 0 {
		out.Outbounds = append(out.Outbounds, map[string]any{"tag": ServiceOutboundTag, "protocol": "freedom"})
		rules = append(rules, map[string]any{"ruleTag": ServiceRuleTag, "user": in.ServiceKeys, "outboundTag": ServiceOutboundTag})
	}
	strategy := "AsIs"
	for i, r := range p.Rules {
		if r.IsPlanRule() {
			continue
		}
		rule := map[string]any{"ruleTag": fmt.Sprintf("sylph-r%d", i+1)}
		if len(r.Domain) > 0 {
			rule["domain"] = geoRefs(r.Domain, in.GeoFile, "geosite")
		}
		if len(r.IP) > 0 {
			rule["ip"] = geoRefs(r.IP, in.GeoFile, "geoip")
			strategy = "IPIfNonMatch"
		}
		if r.Port != "" {
			rule["port"] = r.Port
		}
		if r.Network != "" {
			rule["network"] = r.Network
		}
		if len(r.Protocol) > 0 {
			rule["protocol"] = r.Protocol
		}
		if len(r.Inbound) > 0 {
			rule["inboundTag"] = r.Inbound
		}
		p.target(rule, r.To)
		rules = append(rules, rule)
	}
	if p.isBalancer(p.Default) {
		rules = append(rules, map[string]any{"ruleTag": DefaultRuleTag, "network": "tcp,udp", "balancerTag": p.Default})
	}
	routing := map[string]any{"domainStrategy": strategy, "rules": rules}
	if rules == nil {
		routing["rules"] = []any{}
	}
	var subjects []string
	if len(p.Balancers) > 0 {
		var balancers []any
		for _, b := range p.Balancers {
			bal := map[string]any{"tag": b.Tag, "selector": b.Outbounds, "strategy": map[string]any{"type": b.Strategy}}
			if b.Fallback != "" {
				bal["fallbackTag"] = b.Fallback
			}
			balancers = append(balancers, bal)
			if b.Strategy == StrategyLeastPing {
				for _, tag := range b.Outbounds {
					if !slices.Contains(subjects, tag) {
						subjects = append(subjects, tag)
					}
				}
			}
		}
		routing["balancers"] = balancers
	}
	out.Routing = routing
	if len(subjects) > 0 {
		slices.Sort(subjects)
		out.Observatory = map[string]any{
			"subjectSelector":   subjects,
			"probeUrl":          p.Probe.URL,
			"probeInterval":     p.Probe.Interval,
			"enableConcurrency": true,
		}
	}
	return out, nil
}

func geoRefs(list []string, geoFile func(string) string, builtin string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if _, err := netip.ParsePrefix(v); err == nil {
			out = append(out, v)
			continue
		}
		if _, err := netip.ParseAddr(v); err == nil {
			out = append(out, v)
			continue
		}
		prefix, rest, ok := splitPrefix(v)
		if ok && !slices.Contains(builtinDomain, prefix) && prefix != builtin && geoFile != nil {
			out = append(out, "ext:"+geoFile(prefix)+":"+rest)
			continue
		}
		out = append(out, v)
	}
	return out
}

func renderOutbound(o Outbound) (map[string]any, error) {
	switch o.Kind {
	case KindDirect:
		return map[string]any{"tag": o.Tag, "protocol": "freedom", "settings": map[string]any{"targetStrategy": directStrategies[o.Direct.IP]}}, nil
	case KindBlock:
		return map[string]any{"tag": o.Tag, "protocol": "blackhole"}, nil
	case KindProxy:
		return renderProxy(o.Tag, o.Proxy), nil
	case KindRaw:
		var obj map[string]any
		if err := json.Unmarshal(o.Raw, &obj); err != nil {
			return nil, err
		}
		obj["tag"] = o.Tag
		return obj, nil
	}
	return nil, fmt.Errorf("outbound %q: kind %s cannot be rendered", o.Tag, o.Kind)
}

func tlsSettings(x *Proxy) map[string]any {
	tls := map[string]any{}
	sni := x.SNI
	if sni == "" {
		if _, err := netip.ParseAddr(x.Address); err != nil {
			sni = strings.ToLower(x.Address)
		}
	}
	if sni != "" {
		tls["serverName"] = sni
	}
	if x.Fingerprint != "" {
		tls["fingerprint"] = x.Fingerprint
	}
	return tls
}

func renderProxy(tag string, x *Proxy) map[string]any {
	settings := map[string]any{"address": strings.ToLower(x.Address), "port": x.Port}
	out := map[string]any{"tag": tag, "protocol": x.Protocol, "settings": settings}
	switch x.Protocol {
	case "vless":
		settings["id"] = x.UUID
		settings["encryption"] = "none"
		if x.Flow != "" {
			settings["flow"] = x.Flow
		}
		stream := map[string]any{"network": "raw", "security": x.Security}
		if x.Security == "reality" {
			stream["realitySettings"] = map[string]any{"serverName": x.SNI, "publicKey": x.PublicKey, "shortId": x.ShortID, "fingerprint": x.Fingerprint}
		} else {
			stream["tlsSettings"] = tlsSettings(x)
		}
		out["streamSettings"] = stream
	case "trojan":
		settings["password"] = x.Password
		out["streamSettings"] = map[string]any{"network": "raw", "security": "tls", "tlsSettings": tlsSettings(x)}
	case "shadowsocks":
		settings["method"] = x.Method
		settings["password"] = x.Password
	case "socks", "http":
		if x.Username != "" {
			settings["user"] = x.Username
			settings["pass"] = x.Password
		}
		if x.Security == "tls" {
			out["streamSettings"] = map[string]any{"network": "raw", "security": "tls", "tlsSettings": tlsSettings(x)}
		}
	}
	return out
}

func Tail(p *Profile, planUsers map[int64][]string, generation int64) []TailRule {
	var out []TailRule
	for i, r := range p.Rules {
		if !r.IsPlanRule() {
			continue
		}
		var users []string
		for _, id := range r.Plans {
			users = append(users, planUsers[id]...)
		}
		if len(users) == 0 {
			continue
		}
		slices.Sort(users)
		users = slices.Compact(users)
		rule := TailRule{Tag: fmt.Sprintf("sylph-p%d-r%d", generation, i+1), Users: users}
		if p.isBalancer(r.To) {
			rule.BalancerTag = r.To
		} else {
			rule.OutboundTag = r.To
		}
		out = append(out, rule)
	}
	if len(out) > 0 && p.isBalancer(p.Default) {
		out = append(out, TailRule{Tag: fmt.Sprintf("sylph-p%d-default", generation), CatchAll: true, BalancerTag: p.Default})
	}
	return out
}

func HasPlanRules(p *Profile) bool {
	return slices.ContainsFunc(p.Rules, Rule.IsPlanRule)
}
