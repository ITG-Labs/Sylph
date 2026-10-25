package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

const (
	firstInternalPort = 20001
	xrayAPITag        = "sylph_api"
)

var errRouting = errors.New("routing")

var inboundTagPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

var privateNets = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/3"),
	netip.MustParsePrefix("::/127"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

var localZones = []string{".localhost", ".local", ".internal", ".lan", ".home.arpa", ".localdomain"}

type xrayConfig struct {
	Log         xrayLog       `json:"log"`
	API         xrayAPI       `json:"api"`
	Stats       struct{}      `json:"stats"`
	Policy      xrayPolicy    `json:"policy"`
	Inbounds    []xrayInbound `json:"inbounds"`
	Outbounds   []any         `json:"outbounds"`
	Routing     any           `json:"routing,omitempty"`
	Observatory any           `json:"observatory,omitempty"`
}

type xrayLog struct {
	LogLevel string `json:"loglevel"`
	Access   string `json:"access"`
}

type xrayAPI struct {
	Tag      string   `json:"tag"`
	Listen   string   `json:"listen"`
	Services []string `json:"services"`
}

type xrayPolicy struct {
	Levels map[string]xrayLevel `json:"levels"`
}

type xrayLevel struct {
	StatsUserUplink   bool `json:"statsUserUplink"`
	StatsUserDownlink bool `json:"statsUserDownlink"`
	StatsUserOnline   bool `json:"statsUserOnline"`
}

type xrayInbound struct {
	Tag            string        `json:"tag"`
	Listen         string        `json:"listen"`
	Port           int           `json:"port"`
	Protocol       string        `json:"protocol"`
	Settings       xrayVLESS     `json:"settings"`
	StreamSettings xrayStream    `json:"streamSettings"`
	Sniffing       *xraySniffing `json:"sniffing,omitempty"`
}

type xraySniffing struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
	RouteOnly    bool     `json:"routeOnly"`
}

type xrayVLESS struct {
	Clients    []struct{} `json:"clients"`
	Decryption string     `json:"decryption"`
}

type xrayStream struct {
	Network         string      `json:"network"`
	Security        string      `json:"security"`
	Sockopt         xraySockopt `json:"sockopt"`
	RealitySettings xrayReality `json:"realitySettings"`
}

type xraySockopt struct {
	AcceptProxyProtocol bool `json:"acceptProxyProtocol"`
}

type xrayReality struct {
	Target      string   `json:"target"`
	ServerNames []string `json:"serverNames"`
	PrivateKey  string   `json:"privateKey"`
	ShortIDs    []string `json:"shortIds"`
}

type xrayOutbound struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`
}

type XrayInput struct {
	Inbounds    []Inbound
	APIListen   string
	Routing     *noderoute.Profile
	ServiceKeys []string
	GeoPrefix   string
	Egress      []noderoute.Egress
}

func BuildXray(inbounds []Inbound, apiListen string) ([]byte, error) {
	return BuildXrayRouted(XrayInput{Inbounds: inbounds, APIListen: apiListen})
}

func geoFile(prefix string) func(string) string {
	return func(source string) string { return prefix + "/" + source + ".dat" }
}

func BuildXrayRouted(in XrayInput) ([]byte, error) {
	sorted, err := sortedInbounds(in.Inbounds)
	if err != nil {
		return nil, err
	}
	cfg := xrayConfig{
		Log:       xrayLog{LogLevel: "warning", Access: "none"},
		API:       xrayAPI{Tag: xrayAPITag, Listen: in.APIListen, Services: []string{"HandlerService", "StatsService"}},
		Policy:    xrayPolicy{Levels: map[string]xrayLevel{"0": {StatsUserUplink: true, StatsUserDownlink: true, StatsUserOnline: true}}},
		Inbounds:  make([]xrayInbound, 0, len(sorted)),
		Outbounds: []any{xrayOutbound{Tag: "direct", Protocol: "freedom"}},
	}
	var sniffing *xraySniffing
	if in.Routing != nil {
		profile := *in.Routing
		static, err := noderoute.Render(noderoute.RenderInput{Profile: &profile, ServiceKeys: in.ServiceKeys, GeoFile: geoFile(in.GeoPrefix), Egress: in.Egress})
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errRouting, err)
		}
		if noderoute.HasPlanRules(&profile) {
			cfg.API.Services = append(cfg.API.Services, "RoutingService")
		}
		cfg.Outbounds = make([]any, 0, len(static.Outbounds))
		for _, o := range static.Outbounds {
			cfg.Outbounds = append(cfg.Outbounds, o)
		}
		cfg.Routing = static.Routing
		if static.Observatory != nil {
			cfg.Observatory = static.Observatory
		}
		sniffing = &xraySniffing{Enabled: true, DestOverride: []string{"http", "tls", "quic"}, RouteOnly: true}
	} else if len(in.Egress) > 0 {
		var rules []any
		for _, e := range in.Egress {
			cfg.Outbounds = append(cfg.Outbounds, noderoute.EgressDirect(e.Name, e.SendThrough))
			var users []string
			for _, list := range e.Users {
				users = append(users, list...)
			}
			slices.Sort(users)
			rules = append(rules, map[string]any{"ruleTag": "sylph-e" + e.Name, "user": users, "outboundTag": noderoute.EgressTag(e.Name, "direct")})
		}
		cfg.Routing = map[string]any{"domainStrategy": "AsIs", "rules": rules}
	}
	for i, in := range sorted {
		cfg.Inbounds = append(cfg.Inbounds, xrayInbound{
			Tag:      in.Tag,
			Listen:   "127.0.0.1",
			Port:     firstInternalPort + i,
			Protocol: "vless",
			Settings: xrayVLESS{Clients: []struct{}{}, Decryption: "none"},
			StreamSettings: xrayStream{
				Network:  "raw",
				Security: "reality",
				Sockopt:  xraySockopt{AcceptProxyProtocol: true},
				RealitySettings: xrayReality{
					Target:      in.Reality.Dest,
					ServerNames: in.Reality.ServerNames,
					PrivateKey:  in.Reality.PrivateKey,
					ShortIDs:    in.Reality.ShortIDs,
				},
			},
			Sniffing: sniffing,
		})
	}
	return marshalConfig(cfg)
}

func marshalConfig(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return append(data, '\n'), nil
}

func sortedInbounds(inbounds []Inbound) ([]Inbound, error) {
	out := slices.Clone(inbounds)
	slices.SortFunc(out, func(a, b Inbound) int { return strings.Compare(a.Tag, b.Tag) })
	tags := map[string]bool{}
	names := map[string]string{}
	for _, in := range out {
		if err := checkInbound(in); err != nil {
			return nil, err
		}
		if tags[in.Tag] {
			return nil, fmt.Errorf("inbound %q: duplicate tag", in.Tag)
		}
		tags[in.Tag] = true
		for _, name := range in.Reality.ServerNames {
			if other, ok := names[name]; ok {
				return nil, fmt.Errorf("inbound %q: server name %q is already used by %q", in.Tag, name, other)
			}
			names[name] = in.Tag
		}
	}
	return out, nil
}

func checkInbound(in Inbound) error {
	if !inboundTagPattern.MatchString(in.Tag) {
		return fmt.Errorf("inbound %q: tag must be 1-32 characters of a-z, 0-9 and -", in.Tag)
	}
	if in.Protocol != "vless" || in.Transport != "tcp" || in.Security != "reality" {
		return fmt.Errorf("inbound %q: unsupported kind %s/%s/%s", in.Tag, in.Protocol, in.Transport, in.Security)
	}
	if in.Port < 1 || in.Port > 65535 {
		return fmt.Errorf("inbound %q: port %d out of range", in.Tag, in.Port)
	}
	r := in.Reality
	if r == nil || r.Dest == "" || r.PrivateKey == "" || len(r.ServerNames) == 0 || len(r.ShortIDs) == 0 {
		return fmt.Errorf("inbound %q: incomplete reality settings", in.Tag)
	}
	if !publicDest(r.Dest) {
		return fmt.Errorf("inbound %q: reality dest %q must be a public site", in.Tag, r.Dest)
	}
	return nil
}

func publicDest(dest string) bool {
	host, port, err := net.SplitHostPort(dest)
	if err != nil || port == "" {
		return false
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.WithZone("").Unmap()
		for _, p := range privateNets {
			if p.Contains(addr) {
				return false
			}
		}
		return true
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return false
	}
	if tld := host[strings.LastIndex(host, ".")+1:]; tld == "" || tld[0] < 'a' || tld[0] > 'z' {
		return false
	}
	for _, zone := range localZones {
		if strings.HasSuffix(host, zone) {
			return false
		}
	}
	return true
}
