package clientroute

import (
	"net/netip"
	"regexp"
	"strings"
)

type Client string

const (
	Happ       Client = "happ"
	ITGRay     Client = "itgray"
	SimpleXray Client = "simplexray"
)

var Clients = []Client{Happ, ITGRay, SimpleXray}

type Issue struct {
	Client Client `json:"client"`
	Skip   bool   `json:"skip"`
	Reason string `json:"reason"`
}

type Overlap struct {
	Rule    int    `json:"rule"`
	Earlier int    `json:"earlier"`
	Value   string `json:"value"`
}

var simpleXrayCategory = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)

func isAddress(v string) bool {
	if _, err := netip.ParsePrefix(v); err == nil {
		return true
	}
	_, err := netip.ParseAddr(v)
	return err == nil
}

func hasPrefix(list []string, prefix string) bool {
	for _, v := range list {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}

func badSimpleXrayCategory(r Rule) bool {
	for _, v := range append(append([]string{}, r.Domain...), r.IP...) {
		if !category(v) {
			continue
		}
		_, code, _ := strings.Cut(v, ":")
		if !simpleXrayCategory.MatchString(code) {
			return true
		}
	}
	return false
}

func numericDomain(r Rule) bool {
	for _, v := range r.Domain {
		_, host, _ := strings.Cut(v, ":")
		if (strings.HasPrefix(v, "domain:") || strings.HasPrefix(v, "full:")) && strings.Trim(host, "0123456789.") == "" {
			return true
		}
	}
	return false
}

func Issues(r Rule) []Issue {
	var out []Issue
	for _, c := range Clients {
		if issue, ok := issueOf(r, c); ok {
			out = append(out, issue)
		}
	}
	return out
}

func issueOf(r Rule, c Client) (Issue, bool) {
	skip := func(reason string) (Issue, bool) { return Issue{Client: c, Skip: true, Reason: reason}, true }
	switch c {
	case Happ:
		switch {
		case r.Port != "":
			return skip("port")
		case r.Network != "":
			return skip("network")
		case r.Action == ActionNode:
			return Issue{Client: c, Reason: "node_via_active"}, true
		}
	case SimpleXray:
		switch {
		case r.Port != "":
			return skip("port")
		case r.Network != "":
			return skip("network")
		case hasPrefix(r.Domain, "keyword:"):
			return skip("keyword")
		case hasPrefix(r.Domain, "regexp:"):
			return skip("regexp")
		case badSimpleXrayCategory(r):
			return skip("category_format")
		case numericDomain(r):
			return skip("domain_format")
		}
	}
	return Issue{}, false
}

func skipped(r Rule, c Client) bool {
	issue, ok := issueOf(r, c)
	return ok && issue.Skip
}

func sameAction(a, b string) bool {
	norm := func(s string) string {
		if s == ActionNode {
			return ActionProxy
		}
		return s
	}
	return norm(a) == norm(b)
}

func matchValues(r Rule) []string {
	out := append(append([]string{}, r.Domain...), r.IP...)
	if r.Port != "" {
		out = append(out, "port:"+r.Port)
	}
	return out
}

func Overlaps(rules []Rule) []Overlap {
	var out []Overlap
	for i, r := range rules {
		seen := map[int]bool{}
		for _, v := range matchValues(r) {
			for j := i - 1; j >= 0; j-- {
				if sameAction(rules[j].Action, r.Action) || !containsValue(rules[j], v) {
					continue
				}
				if !seen[j] {
					seen[j] = true
					out = append(out, Overlap{Rule: i, Earlier: j, Value: v})
				}
				break
			}
		}
	}
	return out
}

func containsValue(r Rule, v string) bool {
	for _, x := range matchValues(r) {
		if x == v {
			return true
		}
	}
	return false
}
