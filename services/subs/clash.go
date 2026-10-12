package subs

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

type clashProfile struct {
	MixedPort   int          `yaml:"mixed-port"`
	AllowLAN    bool         `yaml:"allow-lan"`
	Mode        string       `yaml:"mode"`
	LogLevel    string       `yaml:"log-level"`
	Proxies     []clashProxy `yaml:"proxies"`
	ProxyGroups []clashGroup `yaml:"proxy-groups"`
	Rules       []string     `yaml:"rules"`
}

type clashProxy struct {
	Name              string        `yaml:"name"`
	Type              string        `yaml:"type"`
	Server            string        `yaml:"server"`
	Port              int           `yaml:"port"`
	UUID              string        `yaml:"uuid"`
	Network           string        `yaml:"network"`
	UDP               bool          `yaml:"udp"`
	TLS               bool          `yaml:"tls,omitempty"`
	Flow              string        `yaml:"flow,omitempty"`
	ServerName        string        `yaml:"servername,omitempty"`
	ClientFingerprint string        `yaml:"client-fingerprint,omitempty"`
	RealityOpts       *clashReality `yaml:"reality-opts,omitempty"`
}

type clashReality struct {
	PublicKey             string `yaml:"public-key"`
	ShortID               string `yaml:"short-id"`
	SupportX25519MLKEM768 bool   `yaml:"support-x25519mlkem768"`
}

type clashGroup struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Proxies  []string `yaml:"proxies"`
	URL      string   `yaml:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty"`
}

var clashDirectRules = []string{
	"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
	"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,169.254.0.0/16,DIRECT,no-resolve",
	"IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
	"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
	"IP-CIDR6,::1/128,DIRECT,no-resolve",
	"IP-CIDR6,fc00::/7,DIRECT,no-resolve",
	"IP-CIDR6,fe80::/10,DIRECT,no-resolve",
}

func RenderClash(conns []Connection) ([]byte, error) {
	names := profileNames(conns)
	proxies := make([]clashProxy, len(conns))
	for i, c := range conns {
		p := clashProxy{Name: names[i], Type: "vless", Server: c.Address, Port: c.Port, UUID: c.UUID, Network: c.Transport, UDP: true, Flow: c.Flow}
		if c.Security == "reality" {
			p.TLS = true
			p.ServerName = c.SNI
			p.ClientFingerprint = c.Fingerprint
			p.RealityOpts = &clashReality{PublicKey: c.PublicKey, ShortID: c.ShortID, SupportX25519MLKEM768: true}
		}
		proxies[i] = p
	}
	profile := clashProfile{
		MixedPort: 7890,
		Mode:      "rule",
		LogLevel:  "info",
		Proxies:   proxies,
		ProxyGroups: []clashGroup{
			{Name: groupSelect, Type: "select", Proxies: append([]string{groupAuto}, names...)},
			{Name: groupAuto, Type: "url-test", Proxies: names, URL: probeURL, Interval: 300},
		},
		Rules: append(append([]string{}, clashDirectRules...), "MATCH,"+groupSelect),
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(profile); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
