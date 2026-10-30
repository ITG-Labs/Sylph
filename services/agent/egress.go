package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"

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
		if len(out) == maxEgress {
			return nil, fmt.Errorf("at most %d addresses", maxEgress)
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

func buildEgress(status []AddressStatus, users []SyncUser, emailsOf func(SyncUser) []string) []noderoute.Egress {
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
		groups[u.EgressIP][u.PlanID] = append(groups[u.EgressIP][u.PlanID], emailsOf(u)...)
	}
	if len(groups) == 0 {
		return nil
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

const (
	egressProbeEvery = 5 * time.Minute
	egressProbeRetry = time.Minute
	egressProbeURL   = "https://1.1.1.1/cdn-cgi/trace"
	egressCooldown   = 5 * time.Minute
	maxEgress        = 32
)

var egressProbe = func(ctx context.Context, source string) (string, error) {
	dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}, Timeout: 5 * time.Second}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, egressProbeURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 16<<10))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "ip="); ok {
			return v, nil
		}
	}
	return "", errors.New("no ip in the trace answer")
}

type probeResult struct {
	source string
	err    string
	at     time.Time
}

type EgressChecker struct {
	addrs []EgressAddr
	cache map[string]probeResult
	now   func() time.Time
	retry time.Duration
}

func NewEgressChecker(addrs []EgressAddr) *EgressChecker {
	return &EgressChecker{addrs: addrs, cache: map[string]probeResult{}, now: time.Now, retry: 2 * time.Second}
}

func (c *EgressChecker) Status(ctx context.Context) []AddressStatus {
	out := egressStatus(c.addrs)
	for i := range out {
		st := &out[i]
		if !st.OK {
			delete(c.cache, st.IP)
			continue
		}
		r, ok := c.cache[st.IP]
		ttl := egressProbeEvery
		if r.err != "" {
			ttl = egressProbeRetry
		}
		if !ok || r.source != st.Source || c.now().Sub(r.at) >= ttl {
			r = probeResult{source: st.Source, err: c.probe(ctx, st.Source, st.IP), at: c.now()}
			c.cache[st.IP] = r
		}
		if r.err != "" {
			st.OK, st.Error = false, r.err
		}
	}
	return out
}

func (c *EgressChecker) probe(ctx context.Context, source, ip string) string {
	var msg string
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "egress_probe_failed: " + ctx.Err().Error()
			case <-time.After(c.retry):
			}
		}
		seen, err := egressProbe(ctx, source)
		switch {
		case err != nil:
			msg = "egress_probe_failed: " + err.Error()
		case seen != ip:
			return "egress_probe_mismatch: the internet sees " + seen + " instead of " + ip
		default:
			return ""
		}
	}
	return msg
}

func egressNames(list []noderoute.Egress) map[string]string {
	out := map[string]string{}
	for _, e := range list {
		for _, users := range e.Users {
			for _, u := range users {
				out[u] = e.Name
			}
		}
	}
	return out
}

func egressBlocked(applied, desired []noderoute.Egress) map[string]bool {
	want := egressNames(desired)
	out := map[string]bool{}
	for uuid, name := range egressNames(applied) {
		if want[uuid] != name {
			out[uuid] = true
		}
	}
	return out
}
