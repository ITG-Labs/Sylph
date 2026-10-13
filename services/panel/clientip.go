package panel

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

const defaultTrustedProxies = "127.0.0.1/32,::1/128"

type proxyTrust []netip.Prefix

func parseTrustedProxies(s string) (proxyTrust, error) {
	if strings.TrimSpace(s) == "" {
		s = defaultTrustedProxies
	}
	var out proxyTrust
	for part := range strings.SplitSeq(s, ",") {
		p, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q must be a CIDR", strings.TrimSpace(part))
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func (t proxyTrust) trusted(a netip.Addr) bool {
	for _, p := range t {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func (t proxyTrust) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !t.trusted(peer) {
		return peer.String()
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	client := peer
	for _, hop := range slices.Backward(hops) {
		a, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			break
		}
		client = a.Unmap()
		if !t.trusted(client) {
			break
		}
	}
	return client.String()
}
