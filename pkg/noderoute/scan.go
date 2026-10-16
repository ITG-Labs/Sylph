package noderoute

import (
	"net"
	"net/url"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/netaddr"
)

var destinationKeys = map[string]bool{"endpoint": true, "redirect": true, "dest": true, "target": true, "server": true}

func hostOf(v string) string {
	if strings.Contains(v, "://") {
		u, err := url.Parse(v)
		if err != nil {
			return v
		}
		return u.Hostname()
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		return host
	}
	return strings.Trim(v, "[]")
}

func badHost(v string) bool {
	return !netaddr.PublicHost(hostOf(v))
}

func ScanAddresses(v any) string {
	switch x := v.(type) {
	case map[string]any:
		_, hasPort := x["port"]
		for k, val := range x {
			s, isString := val.(string)
			key := strings.ToLower(k)
			if isString && (destinationKeys[key] || (key == "address" && hasPort)) && badHost(s) {
				return s
			}
			if bad := ScanAddresses(val); bad != "" {
				return bad
			}
		}
	case []any:
		for _, val := range x {
			if bad := ScanAddresses(val); bad != "" {
				return bad
			}
		}
	case string:
		if strings.Contains(x, "://") && badHost(x) {
			return x
		}
	}
	return ""
}
