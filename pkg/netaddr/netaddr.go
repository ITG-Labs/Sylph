package netaddr

import (
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

var (
	cgnat         = netip.MustParsePrefix("100.64.0.0/10")
	v4Compatible  = netip.MustParsePrefix("::/96")
	v4Translated  = netip.MustParsePrefix("::ffff:0:0:0/96")
	nat64         = netip.MustParsePrefix("64:ff9b::/96")
	loopbackNames = []string{"localhost", "localhost.localdomain", "ip6-localhost", "ip6-loopback"}
	localZones    = []string{".localhost", ".local", ".internal", ".lan", ".home.arpa", ".localdomain"}
	labelPattern  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

func LetterTLD(host string) bool {
	tld := host[strings.LastIndex(host, ".")+1:]
	return tld != "" && tld[0] >= 'a' && tld[0] <= 'z'
}

func Internal(host string) bool {
	if slices.Contains(loopbackNames, host) || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if addr.Is6() && (v4Compatible.Contains(addr) || v4Translated.Contains(addr) || nat64.Contains(addr)) {
		b := addr.As16()
		embedded := netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
		if !addr.IsUnspecified() && !addr.IsLoopback() && InternalIP(embedded) {
			return true
		}
	}
	return InternalIP(addr)
}

func InternalIP(addr netip.Addr) bool {
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() || cgnat.Contains(addr) ||
		addr == netip.AddrFrom4([4]byte{255, 255, 255, 255})
}

func CanonicalHost(host string) string {
	host = strings.ToLower(host)
	if addr, err := netip.ParseAddr(host); err == nil && addr.Zone() == "" {
		return addr.Unmap().String()
	}
	return host
}

func IsHostname(h string) bool {
	if len(h) == 0 || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if !labelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func PublicHost(host string) bool {
	h := CanonicalHost(strings.TrimSuffix(host, "."))
	if _, err := netip.ParseAddr(h); err == nil {
		return !Internal(h)
	}
	if !IsHostname(h) || !strings.Contains(h, ".") || !LetterTLD(h) || Internal(h) {
		return false
	}
	for _, zone := range localZones {
		if strings.HasSuffix(h, zone) {
			return false
		}
	}
	return true
}
