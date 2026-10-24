package noderoute

import (
	"slices"
	"testing"
)

func TestBalancesDirect(t *testing.T) {
	base := func() *Profile {
		return &Profile{
			Outbounds: []Outbound{
				{Tag: "direct", Kind: KindDirect, Direct: &Direct{IP: "auto"}},
				{Tag: "block", Kind: KindBlock},
				{Tag: "de", Kind: KindProxy, Proxy: &Proxy{Protocol: "socks", Address: "de.example.com", Port: 1080}},
			},
			Default: "direct",
		}
	}
	if got := base().DirectTags(); !slices.Equal(got, []string{"direct"}) {
		t.Fatalf("direct tags = %v", got)
	}
	cases := map[string]struct {
		edit func(p *Profile)
		want bool
	}{
		"no balancers": {func(p *Profile) {}, false},
		"default balancer over direct": {func(p *Profile) {
			p.Balancers = []Balancer{{Tag: "bal", Strategy: StrategyRandom, Outbounds: []string{"direct", "de"}}}
			p.Default = "bal"
		}, true},
		"rule to balancer with direct fallback": {func(p *Profile) {
			p.Balancers = []Balancer{{Tag: "bal", Strategy: StrategyLeastPing, Outbounds: []string{"de"}, Fallback: "direct"}}
			p.Rules = []Rule{{Domain: []string{"geosite:google"}, To: "bal"}}
		}, true},
		"plan rule to balancer over direct": {func(p *Profile) {
			p.Balancers = []Balancer{{Tag: "bal", Strategy: StrategyRandom, Outbounds: []string{"direct"}}}
			p.Rules = []Rule{{Plans: []int64{3}, To: "bal"}}
		}, true},
		"unused balancer over direct": {func(p *Profile) {
			p.Balancers = []Balancer{{Tag: "bal", Strategy: StrategyRandom, Outbounds: []string{"direct", "de"}}}
		}, false},
		"used balancer without direct": {func(p *Profile) {
			p.Balancers = []Balancer{{Tag: "bal", Strategy: StrategyRandom, Outbounds: []string{"de"}, Fallback: "block"}}
			p.Default = "bal"
		}, false},
	}
	for name, c := range cases {
		p := base()
		c.edit(p)
		if got := p.BalancesDirect(); got != c.want {
			t.Errorf("%s: BalancesDirect = %v, want %v", name, got, c.want)
		}
	}
}
