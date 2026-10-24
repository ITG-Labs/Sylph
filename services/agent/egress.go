package agent

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

const egressMissing = "egress_network_missing: no interface of this container has address "

type EgressAddr struct {
	IP     string
	Source string
}

type AddressStatus struct {
	IP    string `json:"ip"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

var interfaceAddrs = net.InterfaceAddrs

func ParseEgress(v string) ([]EgressAddr, error) {
	var out []EgressAddr
	seen := map[string]bool{}
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		pub, src, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("%q: want <public IPv4>=<container IPv4>", item)
		}
		for _, ip := range []string{pub, src} {
			a, err := netip.ParseAddr(ip)
			if err != nil || !a.Is4() || a.String() != ip {
				return nil, fmt.Errorf("%q: %q is not an IPv4 address", item, ip)
			}
		}
		if seen[pub] || seen[src] {
			return nil, fmt.Errorf("%q: address used twice", item)
		}
		seen[pub], seen[src] = true, true
		out = append(out, EgressAddr{IP: pub, Source: src})
	}
	return out, nil
}

func EgressName(ip string) string {
	return strings.ReplaceAll(ip, ".", "-")
}

func egressStatus(addrs []EgressAddr) []AddressStatus {
	out := make([]AddressStatus, 0, len(addrs))
	if len(addrs) == 0 {
		return out
	}
	local := map[string]bool{}
	list, err := interfaceAddrs()
	for _, a := range list {
		if p, perr := netip.ParsePrefix(a.String()); perr == nil {
			local[p.Addr().String()] = true
		}
	}
	for _, a := range addrs {
		st := AddressStatus{IP: a.IP, OK: local[a.Source]}
		switch {
		case err != nil:
			st.OK, st.Error = false, "egress_interfaces_unreadable: "+err.Error()
		case !st.OK:
			st.Error = egressMissing + a.Source
		}
		out = append(out, st)
	}
	return out
}

func buildEgress(addrs []EgressAddr, status []AddressStatus, users []SyncUser) []noderoute.Egress {
	source := map[string]string{}
	for i, a := range addrs {
		if i < len(status) && status[i].OK {
			source[a.IP] = a.Source
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
