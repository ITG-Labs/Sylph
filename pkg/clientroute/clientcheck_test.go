package clientroute

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net"
	"regexp"
	"strings"
	"testing"
)

type itgDomain struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type itgPort struct {
	Single uint16 `json:"single,omitempty"`
	From   uint16 `json:"from,omitempty"`
	To     uint16 `json:"to,omitempty"`
}

type itgRule struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Action     string `json:"action"`
	Conditions struct {
		Processes []string    `json:"processes,omitempty"`
		Domains   []itgDomain `json:"domains,omitempty"`
		IPCIDRs   []string    `json:"ip_cidrs,omitempty"`
		Geo       []string    `json:"geo,omitempty"`
		Ports     []itgPort   `json:"ports,omitempty"`
		Protocols []string    `json:"protocols,omitempty"`
	} `json:"conditions"`
	ServerID   string `json:"server_id,omitempty"`
	ServerName string `json:"server_name,omitempty"`
}

type itgGroup struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Enabled bool      `json:"enabled"`
	Rules   []itgRule `json:"rules"`
}

type itgEnvelope struct {
	V      int        `json:"v"`
	Kind   string     `json:"kind"`
	Name   string     `json:"name"`
	Groups []itgGroup `json:"groups"`
}

func checkITGRay(t *testing.T, link string) itgEnvelope {
	t.Helper()
	const prefix = "itgray://rules/import/"
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("itgray: bad prefix: %.40s", link)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(link, prefix))
	if err != nil {
		t.Fatalf("itgray: base64: %v", err)
	}
	if len(raw) > 64*1024 {
		t.Fatalf("itgray: payload too large: %d", len(raw))
	}
	var env itgEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("itgray: json: %v", err)
	}
	if env.V > 1 || env.Kind != "rules" {
		t.Fatalf("itgray: v=%d kind=%q", env.V, env.Kind)
	}
	n := 0
	for _, g := range env.Groups {
		for _, r := range g.Rules {
			n++
			checkITGRule(t, r)
		}
	}
	if n == 0 || n > 500 {
		t.Fatalf("itgray: %d rules", n)
	}
	return env
}

func checkITGRule(t *testing.T, r itgRule) {
	t.Helper()
	switch r.Action {
	case "proxy", "direct", "block":
	default:
		t.Fatalf("itgray: action %q", r.Action)
	}
	c := r.Conditions
	if len(c.Processes)+len(c.Domains)+len(c.IPCIDRs)+len(c.Geo)+len(c.Ports)+len(c.Protocols) == 0 {
		t.Fatalf("itgray: rule %q has empty conditions", r.Name)
	}
	for _, p := range c.Ports {
		if p.Single != 0 && (p.From != 0 || p.To != 0) {
			t.Fatalf("itgray: port both single and range: %+v", p)
		}
		if p.Single == 0 && (p.From == 0 || p.To == 0 || p.From > p.To) {
			t.Fatalf("itgray: bad range: %+v", p)
		}
	}
	for _, d := range c.Domains {
		switch d.Kind {
		case "exact", "suffix", "keyword", "regex":
		default:
			t.Fatalf("itgray: domain kind %q", d.Kind)
		}
		if d.Value == "" {
			t.Fatal("itgray: empty domain value")
		}
		if d.Kind == "regex" {
			if _, err := regexp.Compile(d.Value); err != nil {
				t.Fatalf("itgray: regex %q: %v", d.Value, err)
			}
		}
	}
	for _, cidr := range c.IPCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil && net.ParseIP(cidr) == nil {
			t.Fatalf("itgray: ip %q", cidr)
		}
	}
	for _, g := range c.Geo {
		if !strings.HasPrefix(g, "geosite:") && !strings.HasPrefix(g, "geoip:") {
			t.Fatalf("itgray: geo %q", g)
		}
	}
	if r.ServerID != "" {
		t.Fatalf("itgray: server_id must not be sent: %q", r.ServerID)
	}
}

type sxBlock struct {
	Target string `json:"target"`
	Text   string `json:"text"`
	Server *struct {
		Name string `json:"name"`
	} `json:"server,omitempty"`
}

type sxPreset struct {
	Format  string `json:"format"`
	Version json.Number
	Routing struct {
		Blocks []sxBlock `json:"blocks"`
	} `json:"routing"`
	GeoipURL   *string `json:"geoipUrl"`
	GeositeURL *string `json:"geositeUrl"`
}

const sxAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

var (
	sxGeosite = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}(@[a-zA-Z0-9][a-zA-Z0-9_-]{0,39}){0,8}$`)
	sxGeoip   = regexp.MustCompile(`^!?[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)
	sxLabel   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

func checkSimpleXray(t *testing.T, link string) sxPreset {
	t.Helper()
	const prefix = "simplexray://routing/"
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("simplexray: bad prefix: %.40s", link)
	}
	payload := strings.TrimPrefix(link, prefix)
	if payload == "" || len(payload)%4 == 1 || strings.Trim(payload, sxAlphabet) != "" {
		t.Fatalf("simplexray: payload alphabet or length")
	}
	last := strings.IndexByte(sxAlphabet, payload[len(payload)-1])
	if (len(payload)%4 == 2 && last&15 != 0) || (len(payload)%4 == 3 && last&3 != 0) {
		t.Fatal("simplexray: non-canonical trailing bits")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("simplexray: base64: %v", err)
	}
	checkStrictJSON(t, raw)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("simplexray: json: %v", err)
	}
	v := string(top["version"])
	if v != "1" && v != "2" {
		t.Fatalf("simplexray: version %s", v)
	}
	var p sxPreset
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("simplexray: decode: %v", err)
	}
	p.Version = json.Number(v)
	if p.Format != "simplexray-routing" {
		t.Fatalf("simplexray: format %q", p.Format)
	}
	blocks := p.Routing.Blocks
	if len(blocks) < 1 || len(blocks) > 32 {
		t.Fatalf("simplexray: %d blocks", len(blocks))
	}
	builtin := map[string]int{}
	compiled := 0
	for _, b := range blocks {
		switch b.Target {
		case "PROXY", "DIRECT", "BLOCK":
		default:
			t.Fatalf("simplexray: target %q", b.Target)
		}
		if b.Server != nil {
			if v != "2" {
				t.Fatal("simplexray: server block needs version 2")
			}
			if b.Target != "PROXY" {
				t.Fatal("simplexray: server block must be PROXY")
			}
			if strings.TrimSpace(b.Server.Name) == "" || len(b.Server.Name) > 512 {
				t.Fatalf("simplexray: server name %q", b.Server.Name)
			}
		} else {
			builtin[b.Target]++
			if builtin[b.Target] > 1 {
				t.Fatalf("simplexray: repeated %s block", b.Target)
			}
		}
		compiled += checkSimpleXrayText(t, b.Text)
	}
	if compiled > 64 {
		t.Fatalf("simplexray: %d compiled rules", compiled)
	}
	for _, u := range []*string{p.GeoipURL, p.GeositeURL} {
		if u != nil && !strings.HasPrefix(*u, "https://") {
			t.Fatalf("simplexray: geo url %q", *u)
		}
	}
	return p
}

func checkSimpleXrayText(t *testing.T, text string) int {
	t.Helper()
	groups := map[string]int{}
	lines := 0
	for _, original := range strings.Split(text, "\n") {
		line := strings.TrimSpace(original)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines++
		kind, value, ok := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		if !ok || value == "" {
			t.Fatalf("simplexray: line %q", line)
		}
		switch kind {
		case "suffix", "domain", "full":
			checkSXDomain(t, value)
			kind = map[string]string{"suffix": "domain", "domain": "domain", "full": "full"}[kind]
		case "ip":
			if _, _, err := net.ParseCIDR(value); err != nil && net.ParseIP(value) == nil {
				t.Fatalf("simplexray: ip %q", value)
			}
		case "geosite":
			if !sxGeosite.MatchString(value) {
				t.Fatalf("simplexray: geosite %q", value)
			}
		case "geoip":
			if !sxGeoip.MatchString(value) {
				t.Fatalf("simplexray: geoip %q", value)
			}
		default:
			t.Fatalf("simplexray: unknown kind %q", kind)
		}
		groups[kind]++
	}
	if lines > 8192 {
		t.Fatalf("simplexray: %d lines", lines)
	}
	n := 0
	for _, c := range groups {
		n += (c + 127) / 128
	}
	return n
}

func checkSXDomain(t *testing.T, v string) {
	t.Helper()
	if strings.ContainsAny(v, " \t/:*") || v != strings.ToLower(v) {
		t.Fatalf("simplexray: domain %q", v)
	}
	digits := true
	for _, label := range strings.Split(v, ".") {
		if label == "" || len(label) > 63 || !sxLabel.MatchString(label) {
			t.Fatalf("simplexray: domain %q", v)
		}
		if strings.Trim(label, "0123456789") != "" {
			digits = false
		}
	}
	if digits {
		t.Fatalf("simplexray: numeric domain %q", v)
	}
}

func checkStrictJSON(t *testing.T, raw []byte) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func(depth int)
	walk = func(depth int) {
		if depth > 64 {
			t.Fatal("simplexray: json too deep")
		}
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("strict json: %v", err)
		}
		switch tok {
		case json.Delim('{'):
			seen := map[string]bool{}
			for dec.More() {
				k, _ := dec.Token()
				key := k.(string)
				if seen[key] {
					t.Fatalf("strict json: duplicate key %q", key)
				}
				seen[key] = true
				walk(depth + 1)
			}
			_, _ = dec.Token()
		case json.Delim('['):
			for dec.More() {
				walk(depth + 1)
			}
			_, _ = dec.Token()
		}
	}
	walk(0)
	if dec.More() {
		t.Fatal("strict json: trailing data")
	}
}

var happStrings = []string{"Name", "GlobalProxy", "RemoteDNSType", "RemoteDNSDomain", "RemoteDNSIP", "DomesticDNSType",
	"DomesticDNSDomain", "DomesticDNSIP", "Geoipurl", "Geositeurl", "LastUpdated", "DomainStrategy", "FakeDNS", "UseChunkFiles"}

var happLists = []string{"DirectSites", "DirectIp", "ProxySites", "ProxyIp", "BlockSites", "BlockIp"}

type happProfile struct {
	Strings  map[string]string
	Lists    map[string][]string
	DnsHosts map[string]string
}

func checkHapp(t *testing.T, link string) happProfile {
	t.Helper()
	const prefix = "happ://routing/onadd/"
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("happ: bad prefix: %.40s", link)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, prefix))
	if err != nil {
		t.Fatalf("happ: base64: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("happ: json: %v", err)
	}
	p := happProfile{Strings: map[string]string{}, Lists: map[string][]string{}}
	for _, k := range happStrings {
		var s string
		if err := json.Unmarshal(top[k], &s); err != nil {
			t.Fatalf("happ: %s must be a string: %s", k, top[k])
		}
		p.Strings[k] = s
	}
	for _, k := range happLists {
		var l []string
		if string(top[k]) == "null" || json.Unmarshal(top[k], &l) != nil {
			t.Fatalf("happ: %s must be a string array: %s", k, top[k])
		}
		p.Lists[k] = l
	}
	if err := json.Unmarshal(top["DnsHosts"], &p.DnsHosts); err != nil {
		t.Fatalf("happ: DnsHosts: %v", err)
	}
	if len(top) != len(happStrings)+len(happLists)+1 {
		t.Fatalf("happ: unexpected keys: %d", len(top))
	}
	return p
}
