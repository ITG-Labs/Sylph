package clientroute

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sampleInput() Input {
	return Input{
		Title:     "Sylph",
		UpdatedAt: time.Unix(1794572865, 0),
		Rules: []Resolved{
			{Rule: Rule{Name: "РФ напрямую", Action: ActionDirect, Domain: []string{"geosite:category-ru"}}},
			{Rule: Rule{Name: "Реклама", Action: ActionBlock, Domain: []string{"geosite:category-ads-all"}}},
			{Rule: Rule{Name: "OpenAI", Action: ActionNode, NodeID: 1, Domain: []string{"domain:openai.com"}}, ServerName: "🇩🇪 Germany"},
			{Rule: Rule{Name: "SMTP", Action: ActionProxy, Port: "25", Domain: []string{"full:smtp.x.ru"}}},
		},
	}
}

func mustRender(t *testing.T, c Client, in Input) string {
	t.Helper()
	link, ok, err := Render(c, in)
	if err != nil || !ok {
		t.Fatalf("render %s: ok=%v err=%v", c, ok, err)
	}
	return link
}

func TestDetectClient(t *testing.T) {
	cases := map[string]Client{
		"Happ/3.8.1":        Happ,
		"happ/1":            Happ,
		"ITGRay/0.2.0":      ITGRay,
		"itgray/0.2.0-beta": ITGRay,
		"SimpleXray/1.13.0": SimpleXray,
	}
	for ua, want := range cases {
		if got, ok := DetectClient(ua); !ok || got != want {
			t.Fatalf("%q: got %q %v, want %q", ua, got, ok, want)
		}
	}
	for _, ua := range []string{"v2rayTun/1", "Mozilla/5.0", "", "Happy/1", "SimpleXrayX/1"} {
		if got, ok := DetectClient(ua); ok {
			t.Fatalf("%q: detected %q", ua, got)
		}
	}
}

func TestRenderHapp(t *testing.T) {
	p := checkHapp(t, mustRender(t, Happ, sampleInput()))
	if !slices.Equal(p.Lists["DirectSites"], []string{"geosite:category-ru"}) {
		t.Fatalf("DirectSites = %v", p.Lists["DirectSites"])
	}
	if !slices.Equal(p.Lists["BlockSites"], []string{"geosite:category-ads-all"}) {
		t.Fatalf("BlockSites = %v", p.Lists["BlockSites"])
	}
	if !slices.Equal(p.Lists["ProxySites"], []string{"domain:openai.com"}) {
		t.Fatalf("ProxySites = %v", p.Lists["ProxySites"])
	}
	wantDirectIP := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "224.0.0.0/4", "255.255.255.255"}
	if !slices.Equal(p.Lists["DirectIp"], wantDirectIP) {
		t.Fatalf("DirectIp = %v", p.Lists["DirectIp"])
	}
	want := map[string]string{
		"Name": "Sylph", "GlobalProxy": "true", "RemoteDNSType": "DoH", "RemoteDNSDomain": "https://cloudflare-dns.com/dns-query",
		"RemoteDNSIP": "1.1.1.1", "DomesticDNSType": "DoU", "DomesticDNSDomain": "", "DomesticDNSIP": "77.88.8.8",
		"Geoipurl": GeoipURL, "Geositeurl": GeositeURL, "LastUpdated": "1794572865", "DomainStrategy": "IPIfNonMatch",
		"FakeDNS": "false", "UseChunkFiles": "true",
	}
	for k, v := range want {
		if p.Strings[k] != v {
			t.Fatalf("%s = %q, want %q", k, p.Strings[k], v)
		}
	}
	if p.DnsHosts["cloudflare-dns.com"] != "1.1.1.1" {
		t.Fatalf("DnsHosts = %v", p.DnsHosts)
	}
}

func TestRenderHappIPAndDedup(t *testing.T) {
	in := Input{Title: "T", UpdatedAt: time.Unix(1, 0), Rules: []Resolved{
		{Rule: Rule{Name: "a", Action: ActionDirect, IP: []string{"geoip:ru", "10.0.0.0/8"}}},
		{Rule: Rule{Name: "b", Action: ActionBlock, IP: []string{"1.2.3.4"}}},
		{Rule: Rule{Name: "c", Action: ActionProxy, IP: []string{"geoip:ru"}}},
	}}
	p := checkHapp(t, mustRender(t, Happ, in))
	if got := p.Lists["DirectIp"]; len(got) != 7 || got[6] != "geoip:ru" {
		t.Fatalf("DirectIp = %v", got)
	}
	if !slices.Equal(p.Lists["BlockIp"], []string{"1.2.3.4"}) || !slices.Equal(p.Lists["ProxyIp"], []string{"geoip:ru"}) {
		t.Fatalf("lists = %v", p.Lists)
	}
	if len(p.Lists["DirectSites"]) != 0 {
		t.Fatalf("DirectSites = %v", p.Lists["DirectSites"])
	}
}

func TestRenderITGRay(t *testing.T) {
	env := checkITGRay(t, mustRender(t, ITGRay, sampleInput()))
	if env.Name != "Sylph" || len(env.Groups) != 1 || env.Groups[0].Name != "Sylph" || !env.Groups[0].Enabled {
		t.Fatalf("envelope = %+v", env)
	}
	rules := env.Groups[0].Rules
	if len(rules) != 4 {
		t.Fatalf("rules = %d", len(rules))
	}
	if rules[0].Name != "РФ напрямую" || rules[0].Action != "direct" || !slices.Equal(rules[0].Conditions.Geo, []string{"geosite:category-ru"}) || !rules[0].Enabled {
		t.Fatalf("rule 0 = %+v", rules[0])
	}
	if rules[2].Action != "proxy" || rules[2].ServerName != "🇩🇪 Germany" || rules[2].Conditions.Domains[0] != (itgDomain{Kind: "suffix", Value: "openai.com"}) {
		t.Fatalf("rule 2 = %+v", rules[2])
	}
	if !slices.Equal(rules[3].Conditions.Ports, []itgPort{{Single: 25}}) || rules[3].Conditions.Domains[0] != (itgDomain{Kind: "exact", Value: "smtp.x.ru"}) {
		t.Fatalf("rule 3 = %+v", rules[3])
	}
	if rules[0].ServerName != "" || rules[3].ServerName != "" {
		t.Fatal("server_name only for rules bound to a server")
	}
}

func TestRenderITGRayConditions(t *testing.T) {
	in := Input{Title: "T", UpdatedAt: time.Unix(1, 0), Rules: []Resolved{
		{Rule: Rule{Name: "kw", Action: ActionBlock, Domain: []string{"keyword:ads", "regexp:^tr\\.", "geosite:youtube"}, Network: "udp"}},
		{Rule: Rule{Name: "ip", Action: ActionDirect, IP: []string{"geoip:ru", "10.0.0.0/8", "1.2.3.4"}, Port: "80,1000-2000"}},
	}}
	rules := checkITGRay(t, mustRender(t, ITGRay, in)).Groups[0].Rules
	wantDomains := []itgDomain{{"keyword", "ads"}, {"regex", "^tr\\."}}
	if !slices.Equal(rules[0].Conditions.Domains, wantDomains) || !slices.Equal(rules[0].Conditions.Geo, []string{"geosite:youtube"}) || !slices.Equal(rules[0].Conditions.Protocols, []string{"udp"}) {
		t.Fatalf("rule 0 = %+v", rules[0].Conditions)
	}
	if !slices.Equal(rules[1].Conditions.Geo, []string{"geoip:ru"}) || !slices.Equal(rules[1].Conditions.IPCIDRs, []string{"10.0.0.0/8", "1.2.3.4"}) {
		t.Fatalf("rule 1 = %+v", rules[1].Conditions)
	}
	if !slices.Equal(rules[1].Conditions.Ports, []itgPort{{Single: 80}, {From: 1000, To: 2000}}) {
		t.Fatalf("ports = %+v", rules[1].Conditions.Ports)
	}
}

func TestRenderSimpleXray(t *testing.T) {
	in := sampleInput()
	in.Rules = append(in.Rules, Resolved{Rule: Rule{Name: "Банки", Action: ActionDirect, Domain: []string{"keyword:bank"}}})
	p := checkSimpleXray(t, mustRender(t, SimpleXray, in))
	if p.Version != "2" {
		t.Fatalf("version = %s", p.Version)
	}
	b := p.Routing.Blocks
	if len(b) != 3 || b[0].Target != "DIRECT" || b[1].Target != "BLOCK" || b[2].Target != "PROXY" || b[2].Server == nil || b[2].Server.Name != "🇩🇪 Germany" {
		t.Fatalf("blocks = %+v", b)
	}
	if b[0].Text != "# РФ напрямую\ngeosite:category-ru" || b[2].Text != "# OpenAI\nsuffix:openai.com" {
		t.Fatalf("texts = %q / %q", b[0].Text, b[2].Text)
	}
	for _, blk := range b {
		if strings.Contains(blk.Text, "bank") || strings.Contains(blk.Text, "smtp") {
			t.Fatalf("skipped rule leaked: %q", blk.Text)
		}
	}
	if p.GeoipURL == nil || *p.GeoipURL != GeoipURL || p.GeositeURL == nil || *p.GeositeURL != GeositeURL {
		t.Fatal("geo urls")
	}
}

func TestRenderSimpleXrayMergesSameTarget(t *testing.T) {
	in := Input{Title: "T", UpdatedAt: time.Unix(1, 0), Rules: []Resolved{
		{Rule: Rule{Name: "a", Action: ActionDirect, Domain: []string{"full:a.ru"}}},
		{Rule: Rule{Name: "b", Action: ActionBlock, IP: []string{"1.2.3.0/24"}}},
		{Rule: Rule{Name: "c", Action: ActionDirect, IP: []string{"geoip:ru"}}},
	}}
	b := checkSimpleXray(t, mustRender(t, SimpleXray, in)).Routing.Blocks
	if len(b) != 2 || b[0].Text != "# a\nfull:a.ru\n# c\ngeoip:ru" || b[1].Text != "# b\nip:1.2.3.0/24" {
		t.Fatalf("blocks = %+v", b)
	}
}

func TestRenderNothingLeft(t *testing.T) {
	in := Input{Title: "T", UpdatedAt: time.Unix(1, 0), Rules: []Resolved{{Rule: Rule{Name: "p", Action: ActionBlock, Port: "25"}}}}
	for _, c := range []Client{Happ, SimpleXray} {
		if link, ok, err := Render(c, in); ok || err != nil || link != "" {
			t.Fatalf("%s: link=%q ok=%v err=%v", c, link, ok, err)
		}
	}
	checkITGRay(t, mustRender(t, ITGRay, in))
	if _, ok, _ := Render(ITGRay, Input{Title: "T"}); ok {
		t.Fatal("empty input must render nothing")
	}
}

func TestRenderNodeWithoutServer(t *testing.T) {
	in := Input{Title: "T", UpdatedAt: time.Unix(1, 0), Rules: []Resolved{{Rule: Rule{Name: "n", Action: ActionNode, NodeID: 3, Domain: []string{"domain:x.ru"}}}}}
	r := checkITGRay(t, mustRender(t, ITGRay, in)).Groups[0].Rules[0]
	if r.ServerName != "" || r.Action != "proxy" {
		t.Fatalf("itgray rule = %+v", r)
	}
	p := checkSimpleXray(t, mustRender(t, SimpleXray, in))
	if p.Version != "1" || len(p.Routing.Blocks) != 1 || p.Routing.Blocks[0].Server != nil || p.Routing.Blocks[0].Target != "PROXY" {
		t.Fatalf("simplexray = %+v", p)
	}
	if got := checkHapp(t, mustRender(t, Happ, in)).Lists["ProxySites"]; !slices.Equal(got, []string{"domain:x.ru"}) {
		t.Fatalf("happ ProxySites = %v", got)
	}
}

func TestRenderTitleWithEmojiAndQuotes(t *testing.T) {
	in := sampleInput()
	in.Title = "VPN \"🔥\" Шлюз"
	links := []string{
		mustRender(t, Happ, in),
		mustRender(t, ITGRay, in),
		mustRender(t, SimpleXray, in),
	}
	if got := checkHapp(t, links[0]).Strings["Name"]; got != in.Title {
		t.Fatalf("happ name = %q", got)
	}
	if got := checkITGRay(t, links[1]).Name; got != in.Title {
		t.Fatalf("itgray name = %q", got)
	}
	checkSimpleXray(t, links[2])
	for _, l := range links {
		for i := 0; i < len(l); i++ {
			if l[i] < 0x21 || l[i] > 0x7e {
				t.Fatalf("non-ascii byte %#x in %.30s", l[i], l)
			}
		}
	}
}

func TestRenderSimpleXrayLimits(t *testing.T) {
	var rules []Resolved
	for i := range 40 {
		rules = append(rules, Resolved{Rule: Rule{Name: "n" + strconv.Itoa(i), Action: ActionNode, NodeID: int64(i + 1), Domain: []string{"domain:a" + strconv.Itoa(i) + ".ru"}}, ServerName: fmt.Sprintf("srv %d", i)})
	}
	p := checkSimpleXray(t, mustRender(t, SimpleXray, Input{Title: "T", UpdatedAt: time.Unix(1, 0), Rules: rules}))
	if len(p.Routing.Blocks) != 32 || p.Routing.Blocks[31].Server.Name != "srv 31" {
		t.Fatalf("blocks = %d", len(p.Routing.Blocks))
	}
	rules = rules[:0]
	for i := range 32 {
		rules = append(rules, Resolved{Rule: Rule{Name: "n", Action: ActionNode, NodeID: int64(i + 1), Domain: []string{"domain:a.ru", "full:b.ru", "geosite:youtube"}}, ServerName: fmt.Sprintf("srv %d", i)})
	}
	p = checkSimpleXray(t, mustRender(t, SimpleXray, Input{Title: "T", UpdatedAt: time.Unix(1, 0), Rules: rules}))
	if len(p.Routing.Blocks) != 21 {
		t.Fatalf("blocks = %d, want 21 (63 compiled rules)", len(p.Routing.Blocks))
	}
}

func TestRenderSimpleXrayNumericDomainSkipped(t *testing.T) {
	in := Input{Title: "T", UpdatedAt: time.Unix(1, 0), Rules: []Resolved{
		{Rule: Rule{Name: "num", Action: ActionDirect, Domain: []string{"domain:1.2.3.4"}}},
		{Rule: Rule{Name: "ok", Action: ActionDirect, Domain: []string{"domain:a.ru"}}},
	}}
	b := checkSimpleXray(t, mustRender(t, SimpleXray, in)).Routing.Blocks
	if len(b) != 1 || b[0].Text != "# ok\nsuffix:a.ru" {
		t.Fatalf("blocks = %+v", b)
	}
	if issues := Issues(in.Rules[0].Rule); len(issues) != 1 || issues[0] != (Issue{Client: SimpleXray, Skip: true, Reason: "domain_format"}) {
		t.Fatalf("issues = %+v", issues)
	}
}
