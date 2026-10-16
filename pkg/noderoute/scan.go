package noderoute

import (
	"net"
	"net/url"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/netaddr"
)

var (
	destinationKeys = []string{"endpoint", "redirect", "dest", "target", "server"}
	forbiddenKeys   = []string{"finalRules", "ipsBlocked"}
)

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

func asciiKey(k string) bool {
	for i := 0; i < len(k); i++ {
		if k[i] >= 0x80 {
			return false
		}
	}
	return true
}

func keyIn(k string, names []string) bool {
	for _, n := range names {
		if strings.EqualFold(k, n) {
			return true
		}
	}
	return false
}

func ScanAddresses(v any) string {
	switch x := v.(type) {
	case map[string]any:
		hasPort := false
		for k := range x {
			if !asciiKey(k) {
				return "key " + k + " is not ASCII"
			}
			if keyIn(k, forbiddenKeys) {
				return "key " + k + " is not allowed"
			}
			hasPort = hasPort || strings.EqualFold(k, "port")
		}
		for k, val := range x {
			s, isString := val.(string)
			if isString && (keyIn(k, destinationKeys) || (strings.EqualFold(k, "address") && hasPort)) && badHost(s) {
				return "points to an internal address: " + s
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
			return "points to an internal address: " + x
		}
	}
	return ""
}
