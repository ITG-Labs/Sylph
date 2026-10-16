package noderoute

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const testUUID = "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b"
const testPublicKey = "Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw"

func goodProfile() Profile {
	return Profile{
		Outbounds: []Outbound{
			{Tag: "direct", Kind: KindDirect, Direct: &Direct{IP: "auto"}},
			{Tag: "block", Kind: KindBlock},
			{Tag: "nl", Kind: KindProxy, Proxy: &Proxy{Protocol: "vless", Address: "nl.example.com", Port: 443, UUID: testUUID,
				Security: "reality", Flow: "xtls-rprx-vision", SNI: "www.microsoft.com", Fingerprint: "chrome", PublicKey: testPublicKey, ShortID: "ab12"}},
			{Tag: "fi", Kind: KindProxy, Proxy: &Proxy{Protocol: "trojan", Address: "1.2.3.4", Port: 8443, Password: "secret", Security: "tls", SNI: "fi.example.com"}},
			{Tag: "ss", Kind: KindProxy, Proxy: &Proxy{Protocol: "shadowsocks", Address: "ss.example.com", Port: 8388, Method: "2022-blake3-aes-128-gcm", Password: "c2VjcmV0c2VjcmV0c2VjcmV0"}},
			{Tag: "sock", Kind: KindProxy, Proxy: &Proxy{Protocol: "socks", Address: "socks.example.com", Port: 1080, Username: "u", Password: "p"}},
			{Tag: "web", Kind: KindProxy, Proxy: &Proxy{Protocol: "http", Address: "proxy.example.com", Port: 3128, Security: "tls"}},
			{Tag: "to-de", Kind: KindCascade, Cascade: &Cascade{NodeID: 1}},
			{Tag: "warp", Kind: KindRaw, Raw: json.RawMessage(`{"protocol":"wireguard","settings":{"secretKey":"x","address":["172.16.0.2/32"],"peers":[{"publicKey":"y","endpoint":"engage.cloudflareclient.com:2408"}]}}`)},
		},
		Balancers: []Balancer{{Tag: "best", Strategy: StrategyLeastPing, Outbounds: []string{"nl", "fi"}, Fallback: "direct"}},
		Rules: []Rule{
			{Name: "Торренты", Protocol: []string{"bittorrent"}, To: "block"},
			{Domain: []string{"geosite:category-ads-all", "example.org", "full:a.b.com", "keyword:ads", "regexp:^x\\.y$", "ru-blocked:ru"}, To: "block"},
			{IP: []string{"geoip:ru", "10.0.0.0/8", "1.1.1.1", "2001:db8::/32", "ru-ips:ru"}, Port: "443,1000-2000", Network: "tcp", Inbound: []string{"main"}, To: "direct"},
			{Plans: []int64{3}, To: "best"},
		},
		Default: "direct",
	}
}

func fieldOf(t *testing.T, err error) string {
	t.Helper()
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want a field error", err)
	}
	return fe.Field
}

func TestValidateAcceptsAndNormalizes(t *testing.T) {
	p := goodProfile()
	p.Rules[1].Domain = append(p.Rules[1].Domain, "Upper.Example.COM")
	p.Outbounds[3].Proxy.SNI = ""
	if err := Validate(&p); err != nil {
		t.Fatal(err)
	}
	if got := p.Rules[1].Domain[1]; got != "domain:example.org" {
		t.Fatalf("bare domain = %q", got)
	}
	if got := p.Rules[1].Domain[len(p.Rules[1].Domain)-1]; got != "domain:upper.example.com" {
		t.Fatalf("upper domain = %q", got)
	}
	if p.Probe.URL != DefaultProbeURL || p.Probe.Interval != DefaultProbeInterval {
		t.Fatalf("probe defaults = %+v", p.Probe)
	}
	if p.Outbounds[0].Direct.IP != "auto" || p.Outbounds[3].Proxy.Fingerprint != "chrome" {
		t.Fatalf("defaults = %+v %+v", p.Outbounds[0].Direct, p.Outbounds[3].Proxy)
	}
	domain, ip := p.Sources()
	if strings.Join(domain, ",") != "ru-blocked" || strings.Join(ip, ",") != "ru-ips" {
		t.Fatalf("sources = %v %v", domain, ip)
	}
	if got := p.Plans(); len(got) != 1 || got[0] != 3 {
		t.Fatalf("plans = %v", got)
	}
	if got := p.CascadeNodes(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("cascade nodes = %v", got)
	}
}

func TestValidateRejects(t *testing.T) {
	for _, c := range []struct {
		name  string
		edit  func(p *Profile)
		field string
	}{
		{"no default", func(p *Profile) { p.Default = "" }, "default"},
		{"unknown default", func(p *Profile) { p.Default = "nope" }, "default"},
		{"bad tag", func(p *Profile) { p.Outbounds[1].Tag = "Block!" }, "outbounds[1].tag"},
		{"reserved tag", func(p *Profile) { p.Outbounds[1].Tag = "sylph-x" }, "outbounds[1].tag"},
		{"duplicate tag", func(p *Profile) { p.Outbounds[1].Tag = "direct" }, "outbounds[1].tag"},
		{"balancer tag clashes", func(p *Profile) { p.Balancers[0].Tag = "nl" }, "balancers[0].tag"},
		{"unknown kind", func(p *Profile) { p.Outbounds[1].Kind = "vmess" }, "outbounds[1].kind"},
		{"direct ip", func(p *Profile) { p.Outbounds[0].Direct.IP = "ipv5" }, "outbounds[0].direct.ip"},
		{"proxy missing", func(p *Profile) { p.Outbounds[2].Proxy = nil }, "outbounds[2].proxy"},
		{"proxy protocol", func(p *Profile) { p.Outbounds[2].Proxy.Protocol = "vmess" }, "outbounds[2].proxy.protocol"},
		{"proxy loopback", func(p *Profile) { p.Outbounds[2].Proxy.Address = "127.0.0.1" }, "outbounds[2].proxy.address"},
		{"proxy metadata", func(p *Profile) { p.Outbounds[2].Proxy.Address = "169.254.169.254" }, "outbounds[2].proxy.address"},
		{"proxy docker name", func(p *Profile) { p.Outbounds[2].Proxy.Address = "core" }, "outbounds[2].proxy.address"},
		{"proxy numeric", func(p *Profile) { p.Outbounds[2].Proxy.Address = "127.1" }, "outbounds[2].proxy.address"},
		{"proxy port", func(p *Profile) { p.Outbounds[2].Proxy.Port = 70000 }, "outbounds[2].proxy.port"},
		{"vless uuid", func(p *Profile) { p.Outbounds[2].Proxy.UUID = "nope" }, "outbounds[2].proxy.uuid"},
		{"vless security", func(p *Profile) { p.Outbounds[2].Proxy.Security = "none" }, "outbounds[2].proxy.security"},
		{"vless flow", func(p *Profile) { p.Outbounds[2].Proxy.Flow = "xtls-rprx-direct" }, "outbounds[2].proxy.flow"},
		{"reality key", func(p *Profile) { p.Outbounds[2].Proxy.PublicKey = "short" }, "outbounds[2].proxy.public_key"},
		{"reality short id", func(p *Profile) { p.Outbounds[2].Proxy.ShortID = "xyz" }, "outbounds[2].proxy.short_id"},
		{"reality sni", func(p *Profile) { p.Outbounds[2].Proxy.SNI = "" }, "outbounds[2].proxy.sni"},
		{"fingerprint", func(p *Profile) { p.Outbounds[2].Proxy.Fingerprint = "netscape" }, "outbounds[2].proxy.fingerprint"},
		{"trojan password", func(p *Profile) { p.Outbounds[3].Proxy.Password = "" }, "outbounds[3].proxy.password"},
		{"ss method", func(p *Profile) { p.Outbounds[4].Proxy.Method = "rc4-md5" }, "outbounds[4].proxy.method"},
		{"socks half creds", func(p *Profile) { p.Outbounds[5].Proxy.Password = "" }, "outbounds[5].proxy.password"},
		{"http security", func(p *Profile) { p.Outbounds[6].Proxy.Security = "reality" }, "outbounds[6].proxy.security"},
		{"control char", func(p *Profile) { p.Outbounds[3].Proxy.Password = "a\nb" }, "outbounds[3].proxy.password"},
		{"cascade node", func(p *Profile) { p.Outbounds[7].Cascade.NodeID = 0 }, "outbounds[7].cascade.node_id"},
		{"raw not object", func(p *Profile) { p.Outbounds[8].Raw = json.RawMessage(`[1]`) }, "outbounds[8].raw"},
		{"raw loopback protocol", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"loopback","settings":{"inboundTag":"x"}}`)
		}, "outbounds[8].raw"},
		{"raw redirect to api", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"freedom","settings":{"redirect":"127.0.0.1:10085"}}`)
		}, "outbounds[8].raw"},
		{"raw private server", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"socks","settings":{"servers":[{"address":"10.0.0.5","port":1080}]}}`)
		}, "outbounds[8].raw"},
		{"raw url to metadata", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"http","settings":{"address":"proxy.example.com","port":80},"note":"http://169.254.169.254/latest"}`)
		}, "outbounds[8].raw"},
		{"raw wireguard endpoint", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"wireguard","settings":{"peers":[{"endpoint":"localhost:51820"}]}}`)
		}, "outbounds[8].raw"},
		{"raw unicode-folded key", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"freedom","settings":{"redirect":"x.example.com:1","addre\u017fs":"127.0.0.1","port":1}}`)
		}, "outbounds[8].raw"},
		{"raw capital port hides address", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"socks","settings":{"Address":"127.0.0.1","Port":10085}}`)
		}, "outbounds[8].raw"},
		{"raw address without a port", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"dns","settings":{"address":"127.0.0.1"}}`)
		}, "outbounds[8].raw"},
		{"raw final rules", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.0/8"]}]}}`)
		}, "outbounds[8].raw"},
		{"raw ips blocked override", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"freedom","settings":{"IPsBlocked":[]}}`)
		}, "outbounds[8].raw"},
		{"raw kelvin key", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"freedom","settings":{"\u212aey":"x"}}`)
		}, "outbounds[8].raw"},
		{"raw redirect with empty host", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"freedom","settings":{"redirect":":10085"}}`)
		}, "outbounds[8].raw"},
		{"raw too big", func(p *Profile) {
			p.Outbounds[8].Raw = json.RawMessage(`{"protocol":"freedom","x":"` + strings.Repeat("a", 17000) + `"}`)
		}, "outbounds[8].raw"},
		{"raw with fields of another kind", func(p *Profile) { p.Outbounds[8].Direct = &Direct{IP: "auto"} }, "outbounds[8].direct"},
		{"balancer strategy", func(p *Profile) { p.Balancers[0].Strategy = "leastLoad" }, "balancers[0].strategy"},
		{"balancer one member", func(p *Profile) { p.Balancers[0].Outbounds = []string{"nl"} }, "balancers[0].outbounds"},
		{"balancer unknown member", func(p *Profile) { p.Balancers[0].Outbounds = []string{"nl", "zz"} }, "balancers[0].outbounds"},
		{"balancer of balancer", func(p *Profile) {
			p.Balancers = append(p.Balancers, Balancer{Tag: "meta", Strategy: StrategyRandom, Outbounds: []string{"best", "nl"}})
		}, "balancers[1].outbounds"},
		{"balancer prefix clash", func(p *Profile) {
			p.Outbounds = append(p.Outbounds, Outbound{Tag: "nl-2", Kind: KindBlock})
		}, "balancers[0].outbounds"},
		{"balancer fallback", func(p *Profile) { p.Balancers[0].Fallback = "best" }, "balancers[0].fallback"},
		{"probe url", func(p *Profile) { p.Probe.URL = "http://x.example.com/" }, "probe.url"},
		{"probe interval", func(p *Profile) { p.Probe.Interval = "1s" }, "probe.interval"},
		{"rule without conditions", func(p *Profile) { p.Rules[0].Protocol = nil }, "rules[0]"},
		{"rule target", func(p *Profile) { p.Rules[0].To = "nope" }, "rules[0].to"},
		{"rule protocol", func(p *Profile) { p.Rules[0].Protocol = []string{"ssh"} }, "rules[0].protocol"},
		{"rule domain prefix", func(p *Profile) { p.Rules[1].Domain = []string{"ext:geosite.dat:ru"} }, "rules[1].domain"},
		{"rule regexp", func(p *Profile) { p.Rules[1].Domain = []string{"regexp:("} }, "rules[1].domain"},
		{"rule domain spaces", func(p *Profile) { p.Rules[1].Domain = []string{"a b"} }, "rules[1].domain"},
		{"rule ip", func(p *Profile) { p.Rules[2].IP = []string{"300.1.1.1"} }, "rules[2].ip"},
		{"rule port", func(p *Profile) { p.Rules[2].Port = "70000" }, "rules[2].port"},
		{"rule port range", func(p *Profile) { p.Rules[2].Port = "20-10" }, "rules[2].port"},
		{"rule network", func(p *Profile) { p.Rules[2].Network = "icmp" }, "rules[2].network"},
		{"rule inbound", func(p *Profile) { p.Rules[2].Inbound = []string{"Main!"} }, "rules[2].inbound"},
		{"plan rule with other conditions", func(p *Profile) { p.Rules[3].Domain = []string{"example.org"} }, "rules[3]"},
		{"plan rule not last", func(p *Profile) { p.Rules = append(p.Rules, Rule{Protocol: []string{"tls"}, To: "direct"}) }, "rules[3]"},
		{"plan id", func(p *Profile) { p.Rules[3].Plans = []int64{0} }, "rules[3].plans"},
		{"rule name", func(p *Profile) { p.Rules[0].Name = strings.Repeat("я", 65) }, "rules[0].name"},
	} {
		p := goodProfile()
		c.edit(&p)
		if got := fieldOf(t, Validate(&p)); got != c.field {
			t.Errorf("%s: field %q, want %q", c.name, got, c.field)
		}
	}
}

func TestValidateRejectsCascadeAfterResolveWhenRequired(t *testing.T) {
	p := goodProfile()
	if err := ValidateResolved(&p); fieldOf(t, err) != "outbounds[7].kind" {
		t.Fatalf("resolved profile with a cascade: %v", err)
	}
}
