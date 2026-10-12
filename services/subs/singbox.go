package subs

import "encoding/json"

type sbProfile struct {
	Log       sbLog   `json:"log"`
	DNS       sbDNS   `json:"dns"`
	Inbounds  []sbTun `json:"inbounds"`
	Outbounds []any   `json:"outbounds"`
	Route     sbRoute `json:"route"`
}

type sbLog struct {
	Level string `json:"level"`
}

type sbDNS struct {
	Servers  []sbDNSServer `json:"servers"`
	Final    string        `json:"final"`
	Strategy string        `json:"strategy"`
}

type sbDNSServer struct {
	Type   string `json:"type"`
	Tag    string `json:"tag"`
	Server string `json:"server,omitempty"`
	Detour string `json:"detour,omitempty"`
}

type sbTun struct {
	Type        string   `json:"type"`
	Tag         string   `json:"tag"`
	Address     []string `json:"address"`
	AutoRoute   bool     `json:"auto_route"`
	StrictRoute bool     `json:"strict_route"`
}

type sbSelector struct {
	Type      string   `json:"type"`
	Tag       string   `json:"tag"`
	Outbounds []string `json:"outbounds"`
	Default   string   `json:"default"`
}

type sbURLTest struct {
	Type      string   `json:"type"`
	Tag       string   `json:"tag"`
	Outbounds []string `json:"outbounds"`
	URL       string   `json:"url"`
	Interval  string   `json:"interval"`
}

type sbVless struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	UUID       string `json:"uuid"`
	Flow       string `json:"flow,omitempty"`
	TLS        *sbTLS `json:"tls,omitempty"`
}

type sbTLS struct {
	Enabled    bool      `json:"enabled"`
	ServerName string    `json:"server_name"`
	UTLS       sbUTLS    `json:"utls"`
	Reality    sbReality `json:"reality"`
}

type sbUTLS struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint"`
}

type sbReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id"`
}

type sbDirect struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}

type sbRoute struct {
	Rules                 []map[string]any `json:"rules"`
	Final                 string           `json:"final"`
	AutoDetectInterface   bool             `json:"auto_detect_interface"`
	DefaultDomainResolver string           `json:"default_domain_resolver"`
}

func RenderSingBox(conns []Connection) ([]byte, error) {
	names := profileNames(conns)
	outbounds := []any{
		sbSelector{Type: "selector", Tag: groupSelect, Outbounds: append([]string{groupAuto}, names...), Default: groupAuto},
		sbURLTest{Type: "urltest", Tag: groupAuto, Outbounds: names, URL: probeURL, Interval: "5m"},
	}
	for i, c := range conns {
		v := sbVless{Type: "vless", Tag: names[i], Server: c.Address, ServerPort: c.Port, UUID: c.UUID, Flow: c.Flow}
		if c.Security == "reality" {
			v.TLS = &sbTLS{
				Enabled:    true,
				ServerName: c.SNI,
				UTLS:       sbUTLS{Enabled: true, Fingerprint: c.Fingerprint},
				Reality:    sbReality{Enabled: true, PublicKey: c.PublicKey, ShortID: c.ShortID},
			}
		}
		outbounds = append(outbounds, v)
	}
	outbounds = append(outbounds, sbDirect{Type: "direct", Tag: "direct"})
	profile := sbProfile{
		Log: sbLog{Level: "warn"},
		DNS: sbDNS{
			Servers: []sbDNSServer{
				{Type: "https", Tag: "dns-remote", Server: "1.1.1.1", Detour: groupSelect},
				{Type: "local", Tag: "dns-local"},
			},
			Final:    "dns-remote",
			Strategy: "ipv4_only",
		},
		Inbounds:  []sbTun{{Type: "tun", Tag: "tun-in", Address: []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"}, AutoRoute: true, StrictRoute: true}},
		Outbounds: outbounds,
		Route: sbRoute{
			Rules: []map[string]any{
				{"action": "sniff"},
				{"protocol": "dns", "action": "hijack-dns"},
				{"ip_is_private": true, "outbound": "direct"},
			},
			Final:                 groupSelect,
			AutoDetectInterface:   true,
			DefaultDomainResolver: "dns-local",
		},
	}
	out, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
