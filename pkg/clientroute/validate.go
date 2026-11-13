package clientroute

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

var (
	domainPrefixes = []string{"domain", "full", "keyword", "regexp", "geosite"}
	actions        = []string{ActionDirect, ActionProxy, ActionBlock, ActionNode}
	portPattern    = regexp.MustCompile(`^[0-9]{1,5}(-[0-9]{1,5})?(,[0-9]{1,5}(-[0-9]{1,5})?)*$`)
)

func fieldErr(field, format string, args ...any) error {
	return &noderoute.FieldError{Field: field, Message: fmt.Sprintf(format, args...)}
}

func lowerPrefix(v string) string {
	if i := strings.Index(v, ":"); i > 0 {
		return strings.ToLower(v[:i]) + v[i:]
	}
	return v
}

func Validate(rules []Rule, tags *Tags) ([]Rule, error) {
	if len(rules) > MaxRules {
		return nil, fieldErr("rules", "at most %d rules", MaxRules)
	}
	out := make([]Rule, len(rules))
	values := 0
	for i, r := range rules {
		field := fmt.Sprintf("rules[%d]", i)
		norm, err := validateRule(field, r, tags)
		if err != nil {
			return nil, err
		}
		values += len(norm.Domain) + len(norm.IP)
		if values > MaxValues {
			return nil, fieldErr("rules", "at most %d values in all rules", MaxValues)
		}
		out[i] = norm
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxBytes {
		return nil, fieldErr("rules", "the rule set must fit in %d bytes", MaxBytes)
	}
	return out, nil
}

func validateRule(field string, r Rule, tags *Tags) (Rule, error) {
	if n := utf8.RuneCountInString(r.Name); n == 0 || n > MaxName {
		return Rule{}, fieldErr(field+".name", "must be 1-%d characters", MaxName)
	}
	if strings.IndexFunc(r.Name, unicode.IsControl) >= 0 {
		return Rule{}, fieldErr(field+".name", "must not contain control characters")
	}
	if !slices.Contains(actions, r.Action) {
		return Rule{}, fieldErr(field+".action", "must be one of %s", strings.Join(actions, ", "))
	}
	if (r.Action == ActionNode) != (r.NodeID > 0) {
		return Rule{}, fieldErr(field+".node_id", "is required for the node action and only for it")
	}
	if len(r.Domain) > 0 && len(r.IP) > 0 {
		return Rule{}, fieldErr(field, "a rule matches either domains or IPs, not both: split it into two rules")
	}
	if len(r.Domain) == 0 && len(r.IP) == 0 && r.Port == "" {
		return Rule{}, fieldErr(field, "needs domains, IPs or a port")
	}
	out := Rule{Name: r.Name, Action: r.Action, NodeID: r.NodeID, Port: r.Port, Network: r.Network}
	var err error
	if out.Domain, err = normalizeList(field+".domain", r.Domain, noderoute.NormalizeDomain, domainPrefixes, tags); err != nil {
		return Rule{}, err
	}
	if out.IP, err = normalizeList(field+".ip", r.IP, noderoute.NormalizeIP, []string{"geoip"}, tags); err != nil {
		return Rule{}, err
	}
	if r.Port != "" {
		if !portPattern.MatchString(r.Port) {
			return Rule{}, fieldErr(field+".port", "ports must be digits, like 443, 80,443 or 1000-2000")
		}
		if err := noderoute.CheckPorts(r.Port); err != nil {
			return Rule{}, fieldErr(field+".port", "%s", err)
		}
	}
	switch r.Network {
	case "", "tcp", "udp":
	default:
		return Rule{}, fieldErr(field+".network", "must be tcp, udp or empty")
	}
	return out, nil
}

func normalizeList(field string, in []string, normalize func(string) (string, error), prefixes []string, tags *Tags) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, len(in))
	for i, v := range in {
		value := lowerPrefix(strings.TrimSpace(v))
		if rest, ok := strings.CutPrefix(value, "suffix:"); ok && slices.Contains(prefixes, "domain") {
			value = "domain:" + rest
		}
		norm, err := normalize(value)
		if err != nil {
			return nil, fieldErr(field, "%q: %s", v, err)
		}
		if strings.Contains(norm, "%") {
			return nil, fieldErr(field, "%q: IPv6 zones are not supported", v)
		}
		if prefix, code, ok := strings.Cut(norm, ":"); ok && !strings.Contains(prefix, ".") {
			if !slices.Contains(prefixes, prefix) && !isAddress(norm) {
				return nil, fieldErr(field, "%q: only %s prefixes are allowed", v, strings.Join(prefixes, ", "))
			}
			if category(norm) && strings.ContainsAny(code, "!@") {
				return nil, fieldErr(field, "%q: negations and attributes are not supported", v)
			}
		}
		if category(norm) {
			if !tags.Loaded() {
				return nil, fieldErr(field, "%q: the runetfreedom category list is not loaded yet, try again later", v)
			}
			if missing := tags.Missing(Rule{Domain: []string{norm}, IP: []string{norm}}); len(missing) > 0 {
				return nil, fieldErr(field, "unknown category %s", norm)
			}
		}
		out[i] = norm
	}
	return out, nil
}
