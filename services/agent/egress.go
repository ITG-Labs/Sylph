package agent

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

var ifacePattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,14}$`)

type EgressAddr struct {
	IP    string
	Iface string
}

type AddressStatus struct {
	IP     string `json:"ip"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Source string `json:"-"`
}

var ifaceAddrs = func(name string) ([]net.Addr, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}
	return iface.Addrs()
}

func ParseEgress(v string) ([]EgressAddr, error) {
	var out []EgressAddr
	seen := map[string]bool{}
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		pub, iface, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("%q: want <public IPv4>=<interface>", item)
		}
		if a, err := netip.ParseAddr(pub); err != nil || !a.Is4() || a.String() != pub {
			return nil, fmt.Errorf("%q: %q is not an IPv4 address", item, pub)
		}
		if !ifacePattern.MatchString(iface) {
			return nil, fmt.Errorf("%q: %q is not an interface name", item, iface)
		}
		if seen["ip "+pub] || seen["if "+iface] {
			return nil, fmt.Errorf("%q: address or interface used twice", item)
		}
		seen["ip "+pub], seen["if "+iface] = true, true
		out = append(out, EgressAddr{IP: pub, Iface: iface})
	}
	return out, nil
}

func EgressName(ip string) string {
	return strings.ReplaceAll(ip, ".", "-")
}

func egressStatus(addrs []EgressAddr) []AddressStatus {
	out := make([]AddressStatus, 0, len(addrs))
	for _, a := range addrs {
		st := AddressStatus{IP: a.IP}
		list, err := ifaceAddrs(a.Iface)
		if err != nil {
			st.Error = "egress_network_missing: interface " + a.Iface + ": " + err.Error()
			out = append(out, st)
			continue
		}
		for _, addr := range list {
			if p, perr := netip.ParsePrefix(addr.String()); perr == nil && p.Addr().Is4() {
				st.OK, st.Source = true, p.Addr().String()
				break
			}
		}
		if !st.OK {
			st.Error = "egress_network_missing: interface " + a.Iface + " has no IPv4 address"
		}
		out = append(out, st)
	}
	return out
}

func buildEgress(status []AddressStatus, users []SyncUser) []noderoute.Egress {
	source := map[string]string{}
	for _, st := range status {
		if st.OK {
			source[st.IP] = st.Source
		}
	}
	groups := map[string]map[int64][]string{}
	for _, u := range users {
		if source[u.EgressIP] == "" {
			continue
		}
		if groups[u.EgressIP] == nil {
			groups[u.EgressIP] = map[int64][]string{}
		}
		groups[u.EgressIP][u.PlanID] = append(groups[u.EgressIP][u.PlanID], u.UUID)
	}
	ips := make([]string, 0, len(groups))
	for ip := range groups {
		ips = append(ips, ip)
	}
	slices.Sort(ips)
	out := make([]noderoute.Egress, 0, len(ips))
	for _, ip := range ips {
		for plan := range groups[ip] {
			slices.Sort(groups[ip][plan])
		}
		out = append(out, noderoute.Egress{Name: EgressName(ip), SendThrough: source[ip], Users: groups[ip]})
	}
	return out
}
