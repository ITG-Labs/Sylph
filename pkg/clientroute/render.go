package clientroute

import (
	"fmt"
	"strings"
	"time"
)

const (
	GeoipURL   = "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geoip.dat"
	GeositeURL = "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geosite.dat"
)

type Resolved struct {
	Rule
	ServerName string
}

type Input struct {
	Title     string
	UpdatedAt time.Time
	Rules     []Resolved
}

var agents = []struct {
	prefix string
	client Client
}{
	{"happ/", Happ},
	{"itgray/", ITGRay},
	{"simplexray/", SimpleXray},
}

func DetectClient(userAgent string) (Client, bool) {
	ua := strings.ToLower(userAgent)
	for _, a := range agents {
		if strings.HasPrefix(ua, a.prefix) {
			return a.client, true
		}
	}
	return "", false
}

func Render(c Client, in Input) (string, bool, error) {
	var rules []Resolved
	for _, r := range in.Rules {
		if !skipped(r.Rule, c) {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		return "", false, nil
	}
	var (
		link string
		err  error
	)
	switch c {
	case Happ:
		link, err = renderHapp(in, rules)
	case ITGRay:
		link, err = renderITGRay(in, rules)
	case SimpleXray:
		link, err = renderSimpleXray(rules)
	default:
		return "", false, fmt.Errorf("unknown client %q", c)
	}
	if err != nil || link == "" {
		return "", false, err
	}
	return link, true, nil
}

func uniqueAppend(list []string, values ...string) []string {
	for _, v := range values {
		if !contains(list, v) {
			list = append(list, v)
		}
	}
	return list
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
