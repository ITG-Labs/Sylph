package noderoute

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/pkg/netaddr"
)

const (
	maxOutbounds    = 64
	maxBalancers    = 16
	maxRules        = 200
	maxListItems    = 1000
	maxTextRunes    = 256
	maxRuleName     = 64
	maxRawBytes     = 16 << 10
	maxPlansPerRule = 50
)

var (
	tagPattern    = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	SourcePattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	Fingerprints = []string{"chrome", "firefox", "safari", "ios", "android", "edge", "random"}
	SSMethods    = []string{"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305",
		"aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "chacha20-ietf-poly1305", "xchacha20-poly1305", "xchacha20-ietf-poly1305"}
	DirectIPs       = []string{"auto", "ipv4", "ipv6", "prefer_ipv4", "prefer_ipv6"}
	Protocols       = []string{"http", "tls", "quic", "bittorrent"}
	builtinDomain   = []string{"full", "domain", "keyword", "regexp", "geosite"}
	builtinIP       = []string{"geoip"}
	reservedSources = []string{"full", "domain", "keyword", "regexp", "geosite", "geoip", "ext", "ext-ip", "ext-domain", "ext-site", "dotless"}
	forbiddenRaw    = []string{"loopback", "dokodemo-door", "tunnel"}
)

type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string {
	return e.Field + ": " + e.Message
}

func fieldErr(field, format string, args ...any) error {
	return &FieldError{Field: field, Message: fmt.Sprintf(format, args...)}
}

func Validate(p *Profile) error {
	return validate(p, true)
}

func ValidateResolved(p *Profile) error {
	return validate(p, false)
}

func ReservedSource(name string) bool {
	return slices.Contains(reservedSources, name)
}

func validate(p *Profile, allowCascade bool) error {
	if len(p.Outbounds) == 0 {
		return fieldErr("outbounds", "at least one outbound is required")
	}
	if len(p.Outbounds) > maxOutbounds {
		return fieldErr("outbounds", "at most %d outbounds", maxOutbounds)
	}
	if len(p.Balancers) > maxBalancers {
		return fieldErr("balancers", "at most %d balancers", maxBalancers)
	}
	if len(p.Rules) > maxRules {
		return fieldErr("rules", "at most %d rules", maxRules)
	}
	outbounds := map[string]bool{}
	tags := map[string]bool{}
	for i := range p.Outbounds {
		o := &p.Outbounds[i]
		field := fmt.Sprintf("outbounds[%d]", i)
		if err := checkTag(field+".tag", o.Tag, tags); err != nil {
			return err
		}
		if err := checkOutbound(field, o, allowCascade); err != nil {
			return err
		}
		outbounds[o.Tag] = true
	}
	balancers := map[string]bool{}
	for i := range p.Balancers {
		b := &p.Balancers[i]
		field := fmt.Sprintf("balancers[%d]", i)
		if err := checkTag(field+".tag", b.Tag, tags); err != nil {
			return err
		}
		balancers[b.Tag] = true
	}
	leastPing := false
	for i := range p.Balancers {
		b := &p.Balancers[i]
		field := fmt.Sprintf("balancers[%d]", i)
		if err := checkBalancer(field, b, outbounds); err != nil {
			return err
		}
		leastPing = leastPing || b.Strategy == StrategyLeastPing
	}
	if err := checkProbe(&p.Probe); err != nil {
		return err
	}
	_ = leastPing
	planSeen := false
	for i := range p.Rules {
		r := &p.Rules[i]
		field := fmt.Sprintf("rules[%d]", i)
		if err := checkRule(field, r, tags); err != nil {
			return err
		}
		if r.IsPlanRule() {
			planSeen = true
		} else if planSeen {
			return fieldErr(fmt.Sprintf("rules[%d]", i-1), "rules by plan must come after all other rules")
		}
	}
	if p.Default == "" || !tags[p.Default] {
		return fieldErr("default", "must be the tag of an outbound or a balancer")
	}
	return nil
}

func checkTag(field, tag string, seen map[string]bool) error {
	if !tagPattern.MatchString(tag) {
		return fieldErr(field, "must be 1-32 characters of a-z, 0-9 and -")
	}
	if strings.HasPrefix(tag, "sylph") {
		return fieldErr(field, "tags starting with sylph are reserved")
	}
	if seen[tag] {
		return fieldErr(field, "tag %q is used twice", tag)
	}
	seen[tag] = true
	return nil
}

func checkOutbound(field string, o *Outbound, allowCascade bool) error {
	present := map[string]bool{KindDirect: o.Direct != nil, KindProxy: o.Proxy != nil, KindCascade: o.Cascade != nil, KindRaw: len(o.Raw) > 0}
	for kind, set := range present {
		if set && kind != o.Kind {
			return fieldErr(field+"."+kind, "must be empty for a %s outbound", o.Kind)
		}
	}
	switch o.Kind {
	case KindDirect:
		if o.Direct == nil {
			o.Direct = &Direct{}
		}
		if o.Direct.IP == "" {
			o.Direct.IP = "auto"
		}
		if !slices.Contains(DirectIPs, o.Direct.IP) {
			return fieldErr(field+".direct.ip", "must be one of %s", strings.Join(DirectIPs, ", "))
		}
	case KindBlock:
	case KindProxy:
		if o.Proxy == nil {
			return fieldErr(field+".proxy", "is required")
		}
		return checkProxy(field+".proxy", o.Proxy)
	case KindCascade:
		if !allowCascade {
			return fieldErr(field+".kind", "cascade must be resolved before it reaches a node")
		}
		if o.Cascade == nil || o.Cascade.NodeID <= 0 {
			return fieldErr(field+".cascade.node_id", "must be a node id")
		}
	case KindRaw:
		return checkRaw(field+".raw", o.Raw)
	default:
		return fieldErr(field+".kind", "must be direct, block, proxy, cascade or raw")
	}
	return nil
}

func checkText(field, v string, required bool) error {
	if v == "" {
		if required {
			return fieldErr(field, "is required")
		}
		return nil
	}
	if utf8.RuneCountInString(v) > maxTextRunes {
		return fieldErr(field, "must be at most %d characters", maxTextRunes)
	}
	for _, r := range v {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fieldErr(field, "must not contain control characters")
		}
	}
	return nil
}

func checkHostname(field, v string, required bool) error {
	if v == "" {
		if required {
			return fieldErr(field, "is required")
		}
		return nil
	}
	if !netaddr.IsHostname(strings.ToLower(v)) || !strings.Contains(v, ".") {
		return fieldErr(field, "must be a hostname")
	}
	return nil
}

func checkProxy(field string, x *Proxy) error {
	x.Protocol = strings.ToLower(x.Protocol)
	x.Security = strings.ToLower(x.Security)
	if !netaddr.PublicHost(x.Address) {
		return fieldErr(field+".address", "must be a public hostname or IP")
	}
	if x.Port < 1 || x.Port > 65535 {
		return fieldErr(field+".port", "must be 1-65535")
	}
	for _, f := range []struct{ name, v string }{{"username", x.Username}, {"password", x.Password}, {"uuid", x.UUID}} {
		if err := checkText(field+"."+f.name, f.v, false); err != nil {
			return err
		}
	}
	if x.Fingerprint != "" && !slices.Contains(Fingerprints, x.Fingerprint) {
		return fieldErr(field+".fingerprint", "must be one of %s", strings.Join(Fingerprints, ", "))
	}
	switch x.Protocol {
	case "vless":
		x.UUID = strings.ToLower(x.UUID)
		if !uuidPattern.MatchString(x.UUID) {
			return fieldErr(field+".uuid", "must be a UUID")
		}
		if x.Security != "reality" && x.Security != "tls" {
			return fieldErr(field+".security", "must be reality or tls")
		}
		if x.Flow != "" && x.Flow != "xtls-rprx-vision" {
			return fieldErr(field+".flow", "must be empty or xtls-rprx-vision")
		}
		if x.Security == "reality" {
			if b, err := base64.RawURLEncoding.DecodeString(x.PublicKey); err != nil || len(b) != 32 {
				return fieldErr(field+".public_key", "must be 32 bytes in unpadded base64url")
			}
			x.ShortID = strings.ToLower(x.ShortID)
			if _, err := hex.DecodeString(x.ShortID); err != nil || len(x.ShortID) > 16 || len(x.ShortID)%2 != 0 {
				return fieldErr(field+".short_id", "must be hex of even length up to 16")
			}
			if err := checkHostname(field+".sni", x.SNI, true); err != nil {
				return err
			}
			if x.Fingerprint == "" {
				x.Fingerprint = "chrome"
			}
		} else if err := checkHostname(field+".sni", x.SNI, false); err != nil {
			return err
		}
	case "trojan":
		if err := checkText(field+".password", x.Password, true); err != nil {
			return err
		}
		if x.Security == "" {
			x.Security = "tls"
		}
		if x.Security != "tls" {
			return fieldErr(field+".security", "must be tls")
		}
		if err := checkHostname(field+".sni", x.SNI, false); err != nil {
			return err
		}
		if x.Fingerprint == "" {
			x.Fingerprint = "chrome"
		}
	case "shadowsocks":
		if !slices.Contains(SSMethods, x.Method) {
			return fieldErr(field+".method", "must be one of %s", strings.Join(SSMethods, ", "))
		}
		if err := checkText(field+".password", x.Password, true); err != nil {
			return err
		}
		if x.Security != "" {
			return fieldErr(field+".security", "must be empty")
		}
	case "socks", "http":
		if (x.Username == "") != (x.Password == "") {
			if x.Username == "" {
				return fieldErr(field+".username", "is required together with password")
			}
			return fieldErr(field+".password", "is required together with username")
		}
		if x.Protocol == "socks" && x.Security != "" {
			return fieldErr(field+".security", "must be empty")
		}
		if x.Protocol == "http" && x.Security != "" && x.Security != "tls" {
			return fieldErr(field+".security", "must be empty or tls")
		}
		if err := checkHostname(field+".sni", x.SNI, false); err != nil {
			return err
		}
	default:
		return fieldErr(field+".protocol", "must be vless, trojan, shadowsocks, socks or http")
	}
	if x.Protocol != "vless" && (x.UUID != "" || x.Flow != "" || x.PublicKey != "" || x.ShortID != "") {
		return fieldErr(field+".uuid", "uuid, flow, public_key and short_id are only for vless")
	}
	if x.Protocol != "shadowsocks" && x.Method != "" {
		return fieldErr(field+".method", "is only for shadowsocks")
	}
	return nil
}

func checkRaw(field string, raw json.RawMessage) error {
	if len(raw) > maxRawBytes {
		return fieldErr(field, "must be at most %d bytes", maxRawBytes)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return fieldErr(field, "must be a JSON object")
	}
	proto, _ := obj["protocol"].(string)
	if proto == "" {
		return fieldErr(field, "must have a protocol")
	}
	if slices.Contains(forbiddenRaw, strings.ToLower(proto)) {
		return fieldErr(field, "protocol %s is not allowed", proto)
	}
	if bad := ScanAddresses(obj); bad != "" {
		return fieldErr(field, "points to an internal address: %s", bad)
	}
	return nil
}

func checkBalancer(field string, b *Balancer, outbounds map[string]bool) error {
	if b.Strategy == "" {
		b.Strategy = StrategyRandom
	}
	if b.Strategy != StrategyRandom && b.Strategy != StrategyRoundRobin && b.Strategy != StrategyLeastPing {
		return fieldErr(field+".strategy", "must be random, roundRobin or leastPing")
	}
	if len(b.Outbounds) < 2 {
		return fieldErr(field+".outbounds", "needs at least two outbounds")
	}
	seen := map[string]bool{}
	for _, tag := range b.Outbounds {
		if !outbounds[tag] {
			return fieldErr(field+".outbounds", "%q is not an outbound", tag)
		}
		if seen[tag] {
			return fieldErr(field+".outbounds", "%q is listed twice", tag)
		}
		seen[tag] = true
	}
	for _, tag := range b.Outbounds {
		for other := range outbounds {
			if other != tag && strings.HasPrefix(other, tag) {
				return fieldErr(field+".outbounds", "tag %q is the beginning of %q: xray would pick both, rename one", tag, other)
			}
		}
	}
	if b.Fallback != "" && !outbounds[b.Fallback] {
		return fieldErr(field+".fallback", "must be an outbound")
	}
	return nil
}

func checkProbe(p *Probe) error {
	if p.URL == "" {
		p.URL = DefaultProbeURL
	}
	if p.Interval == "" {
		p.Interval = DefaultProbeInterval
	}
	u, err := url.Parse(p.URL)
	if err != nil || u.Scheme != "https" || !netaddr.PublicHost(u.Hostname()) || len(p.URL) > 512 {
		return fieldErr("probe.url", "must be a public https URL")
	}
	d, err := time.ParseDuration(p.Interval)
	if err != nil || d < 10*time.Second || d > time.Hour {
		return fieldErr("probe.interval", "must be a duration from 10s to 1h")
	}
	return nil
}

func checkRule(field string, r *Rule, targets map[string]bool) error {
	if utf8.RuneCountInString(r.Name) > maxRuleName {
		return fieldErr(field+".name", "must be at most %d characters", maxRuleName)
	}
	if err := checkText(field+".name", r.Name, false); err != nil {
		return err
	}
	general := len(r.Domain) > 0 || len(r.IP) > 0 || r.Port != "" || r.Network != "" || len(r.Protocol) > 0 || len(r.Inbound) > 0
	if r.IsPlanRule() {
		if general {
			return fieldErr(field, "a rule by plan has no other conditions")
		}
		if len(r.Plans) > maxPlansPerRule {
			return fieldErr(field+".plans", "at most %d plans", maxPlansPerRule)
		}
		for _, id := range r.Plans {
			if id <= 0 {
				return fieldErr(field+".plans", "must be plan ids")
			}
		}
	} else if !general {
		return fieldErr(field, "needs at least one condition")
	}
	for _, list := range [][]string{r.Domain, r.IP, r.Protocol, r.Inbound} {
		if len(list) > maxListItems {
			return fieldErr(field, "lists must have at most %d items", maxListItems)
		}
	}
	for i, d := range r.Domain {
		norm, err := normalizeDomain(d)
		if err != nil {
			return fieldErr(field+".domain", "%q: %s", d, err)
		}
		r.Domain[i] = norm
	}
	for i, v := range r.IP {
		norm, err := normalizeIP(v)
		if err != nil {
			return fieldErr(field+".ip", "%q: %s", v, err)
		}
		r.IP[i] = norm
	}
	if r.Port != "" {
		if err := checkPorts(r.Port); err != nil {
			return fieldErr(field+".port", "%s", err)
		}
	}
	switch r.Network {
	case "", "tcp", "udp", "tcp,udp":
	default:
		return fieldErr(field+".network", "must be tcp, udp or tcp,udp")
	}
	for _, p := range r.Protocol {
		if !slices.Contains(Protocols, p) {
			return fieldErr(field+".protocol", "must be from %s", strings.Join(Protocols, ", "))
		}
	}
	for _, tag := range r.Inbound {
		if !tagPattern.MatchString(tag) {
			return fieldErr(field+".inbound", "must be inbound tags")
		}
	}
	if !targets[r.To] {
		return fieldErr(field+".to", "must be the tag of an outbound or a balancer")
	}
	return nil
}

type valueError string

func (e valueError) Error() string { return string(e) }

func splitPrefix(v string) (string, string, bool) {
	i := strings.Index(v, ":")
	if i <= 0 {
		return "", v, false
	}
	return v[:i], v[i+1:], true
}

func normalizeDomain(v string) (string, error) {
	if v == "" || len(v) > 512 || strings.ContainsAny(v, " \t\r\n") {
		return "", valueError("must be a domain rule without spaces")
	}
	prefix, rest, ok := splitPrefix(v)
	if !ok {
		if !netaddr.IsHostname(strings.ToLower(v)) {
			return "", valueError("must be a domain or prefixed rule")
		}
		return "domain:" + strings.ToLower(v), nil
	}
	if rest == "" {
		return "", valueError("empty value")
	}
	switch prefix {
	case "full", "domain":
		if !netaddr.IsHostname(strings.ToLower(rest)) {
			return "", valueError("must be a domain")
		}
		return prefix + ":" + strings.ToLower(rest), nil
	case "keyword":
		return v, nil
	case "regexp":
		if _, err := regexp.Compile(rest); err != nil {
			return "", valueError("bad regexp")
		}
		return v, nil
	case "geosite":
		if !geoCodeOK(rest) {
			return "", valueError("bad geosite code")
		}
		return "geosite:" + strings.ToLower(rest), nil
	}
	if ReservedSource(prefix) || !SourcePattern.MatchString(prefix) {
		return "", valueError("unknown prefix")
	}
	if !geoCodeOK(rest) {
		return "", valueError("bad code")
	}
	return prefix + ":" + strings.ToLower(rest), nil
}

func normalizeIP(v string) (string, error) {
	if pfx, err := netip.ParsePrefix(v); err == nil {
		return pfx.Masked().String(), nil
	}
	if addr, err := netip.ParseAddr(v); err == nil {
		return addr.String(), nil
	}
	prefix, rest, ok := splitPrefix(v)
	if !ok || rest == "" || !geoCodeOK(rest) {
		return "", valueError("must be an IP, a CIDR, geoip:<code> or <source>:<code>")
	}
	if prefix == "geoip" {
		return "geoip:" + strings.ToLower(rest), nil
	}
	if ReservedSource(prefix) || !SourcePattern.MatchString(prefix) {
		return "", valueError("unknown prefix")
	}
	return prefix + ":" + strings.ToLower(rest), nil
}

var geoCodePattern = regexp.MustCompile(`^[A-Za-z0-9_.!@-]{1,64}$`)

func geoCodeOK(code string) bool {
	return geoCodePattern.MatchString(code)
}

func checkPorts(s string) error {
	for part := range strings.SplitSeq(s, ",") {
		lo, hi, isRange := strings.Cut(strings.TrimSpace(part), "-")
		a, err := strconv.Atoi(lo)
		if err != nil || a < 1 || a > 65535 {
			return valueError("ports must be 1-65535, like 443 or 1000-2000")
		}
		if isRange {
			b, err := strconv.Atoi(hi)
			if err != nil || b < a || b > 65535 {
				return valueError("ranges must go up, like 1000-2000")
			}
		}
	}
	return nil
}

func (p *Profile) Sources() (domain, ip []string) {
	add := func(list []string, out *[]string, builtin []string) {
		for _, v := range list {
			if _, err := netip.ParsePrefix(v); err == nil {
				continue
			}
			if _, err := netip.ParseAddr(v); err == nil {
				continue
			}
			prefix, _, ok := splitPrefix(v)
			if ok && !slices.Contains(builtin, prefix) && !slices.Contains(*out, prefix) {
				*out = append(*out, prefix)
			}
		}
	}
	for _, r := range p.Rules {
		add(r.Domain, &domain, builtinDomain)
		add(r.IP, &ip, builtinIP)
	}
	slices.Sort(domain)
	slices.Sort(ip)
	return domain, ip
}

func (p *Profile) Plans() []int64 {
	var out []int64
	for _, r := range p.Rules {
		for _, id := range r.Plans {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	return out
}

func (p *Profile) CascadeNodes() []int64 {
	var out []int64
	for _, o := range p.Outbounds {
		if o.Kind == KindCascade && o.Cascade != nil && !slices.Contains(out, o.Cascade.NodeID) {
			out = append(out, o.Cascade.NodeID)
		}
	}
	slices.Sort(out)
	return out
}
