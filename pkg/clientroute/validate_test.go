package clientroute

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

func testTags() *Tags {
	return &Tags{
		Release:   "202611131125",
		Geosite:   []string{"category-ads-all", "category-ru", "google", "youtube"},
		Geoip:     []string{"private", "ru"},
		FetchedAt: time.Unix(1794572865, 0),
	}
}

func fieldOf(t *testing.T, err error) string {
	t.Helper()
	var fe *noderoute.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("want *noderoute.FieldError, got %T %v", err, err)
	}
	return fe.Field
}

func TestValidateNormalizes(t *testing.T) {
	out, err := Validate([]Rule{{Name: "ru", Action: ActionDirect, Domain: []string{"YouTube.com", "GEOSITE:Category-RU"}}}, testTags())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"domain:youtube.com", "geosite:category-ru"}
	if !slices.Equal(out[0].Domain, want) {
		t.Fatalf("domain = %v, want %v", out[0].Domain, want)
	}
}

func TestValidateDoesNotMutateInput(t *testing.T) {
	in := []Rule{{Name: "ru", Action: ActionDirect, Domain: []string{"YouTube.com"}}}
	if _, err := Validate(in, testTags()); err != nil {
		t.Fatal(err)
	}
	if in[0].Domain[0] != "YouTube.com" {
		t.Fatalf("input mutated: %v", in[0].Domain)
	}
}

func TestValidateGeoCodes(t *testing.T) {
	cases := []struct {
		rule  Rule
		field string
	}{
		{Rule{Name: "a", Action: ActionDirect, Domain: []string{"geosite:google@cn"}}, "rules[0].domain"},
		{Rule{Name: "a", Action: ActionDirect, IP: []string{"geoip:!ru"}}, "rules[0].ip"},
		{Rule{Name: "a", Action: ActionDirect, Domain: []string{"ext:file:tag"}}, "rules[0].domain"},
		{Rule{Name: "a", Action: ActionDirect, IP: []string{"ext-ip:x"}}, "rules[0].ip"},
		{Rule{Name: "a", Action: ActionDirect, Domain: []string{"mysource:tag"}}, "rules[0].domain"},
	}
	for _, c := range cases {
		_, err := Validate([]Rule{c.rule}, testTags())
		if err == nil {
			t.Fatalf("%v: want error", c.rule)
		}
		if got := fieldOf(t, err); got != c.field {
			t.Fatalf("%v: field = %q, want %q", c.rule, got, c.field)
		}
	}
}

func TestValidateDomainAndIPTogether(t *testing.T) {
	_, err := Validate([]Rule{{Name: "a", Action: ActionBlock, Domain: []string{"youtube.com"}, IP: []string{"geoip:ru"}}}, testTags())
	if err == nil || fieldOf(t, err) != "rules[0]" {
		t.Fatalf("want error on rules[0], got %v", err)
	}
}

func TestValidateNeedsCondition(t *testing.T) {
	_, err := Validate([]Rule{{Name: "a", Action: ActionBlock, Network: "udp"}}, testTags())
	if err == nil || fieldOf(t, err) != "rules[0]" {
		t.Fatalf("want error on rules[0], got %v", err)
	}
	if _, err := Validate([]Rule{{Name: "a", Action: ActionBlock, Port: "25"}}, testTags()); err != nil {
		t.Fatalf("port-only rule: %v", err)
	}
}

func TestValidateNodeID(t *testing.T) {
	for _, r := range []Rule{
		{Name: "a", Action: ActionNode, Domain: []string{"a.ru"}},
		{Name: "a", Action: ActionProxy, NodeID: 5, Domain: []string{"a.ru"}},
	} {
		_, err := Validate([]Rule{r}, testTags())
		if err == nil || fieldOf(t, err) != "rules[0].node_id" {
			t.Fatalf("%v: want error on rules[0].node_id, got %v", r, err)
		}
	}
	if _, err := Validate([]Rule{{Name: "a", Action: ActionNode, NodeID: 5, Domain: []string{"a.ru"}}}, testTags()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAction(t *testing.T) {
	_, err := Validate([]Rule{{Name: "a", Action: "reject", Domain: []string{"a.ru"}}}, testTags())
	if err == nil || fieldOf(t, err) != "rules[0].action" {
		t.Fatalf("want error on rules[0].action, got %v", err)
	}
}

func TestValidateLimits(t *testing.T) {
	many := make([]Rule, MaxRules+1)
	for i := range many {
		many[i] = Rule{Name: "a", Action: ActionDirect, Domain: []string{"a.ru"}}
	}
	if _, err := Validate(many, testTags()); err == nil || fieldOf(t, err) != "rules" {
		t.Fatalf("65 rules: %v", err)
	}
	values := make([]string, MaxValues+1)
	for i := range values {
		values[i] = "full:a" + strings.Repeat("b", i%5) + ".ru"
	}
	if _, err := Validate([]Rule{{Name: "a", Action: ActionDirect, Domain: values}}, testTags()); err == nil || fieldOf(t, err) != "rules" {
		t.Fatalf("1001 values: %v", err)
	}
	long := strings.Repeat("я", MaxName+1)
	if _, err := Validate([]Rule{{Name: long, Action: ActionDirect, Domain: []string{"a.ru"}}}, testTags()); err == nil || fieldOf(t, err) != "rules[0].name" {
		t.Fatalf("long name: %v", err)
	}
	if _, err := Validate([]Rule{{Name: "", Action: ActionDirect, Domain: []string{"a.ru"}}}, testTags()); err == nil || fieldOf(t, err) != "rules[0].name" {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := Validate([]Rule{{Name: "a\nb", Action: ActionDirect, Domain: []string{"a.ru"}}}, testTags()); err == nil || fieldOf(t, err) != "rules[0].name" {
		t.Fatalf("control char in name: %v", err)
	}
}

func TestValidateBytes(t *testing.T) {
	rules := make([]Rule, 40)
	for i := range rules {
		vals := make([]string, 20)
		for j := range vals {
			vals[j] = "full:" + strings.Repeat("x", 10) + "-" + strings.Repeat("y", j+1) + ".example.com"
		}
		rules[i] = Rule{Name: strings.Repeat("n", 60), Action: ActionDirect, Domain: vals}
	}
	if _, err := Validate(rules, testTags()); err == nil || fieldOf(t, err) != "rules" {
		t.Fatalf("over 16 KiB: %v", err)
	}
}

func TestValidateTagsNotLoaded(t *testing.T) {
	_, err := Validate([]Rule{{Name: "a", Action: ActionDirect, Domain: []string{"geosite:category-ru"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("want not loaded error, got %v", err)
	}
	if fieldOf(t, err) != "rules[0].domain" {
		t.Fatalf("field = %q", fieldOf(t, err))
	}
	if _, err := Validate([]Rule{{Name: "a", Action: ActionDirect, Domain: []string{"full:a.ru"}}}, nil); err != nil {
		t.Fatalf("rule without categories: %v", err)
	}
	if _, err := Validate([]Rule{{Name: "a", Action: ActionDirect, IP: []string{"geoip:ru"}}}, &Tags{}); err == nil || fieldOf(t, err) != "rules[0].ip" {
		t.Fatalf("empty tags: %v", err)
	}
}

func TestValidateUnknownCategory(t *testing.T) {
	_, err := Validate([]Rule{{Name: "a", Action: ActionDirect, IP: []string{"geoip:zz"}}}, testTags())
	if err == nil || !strings.Contains(err.Error(), "unknown category geoip:zz") {
		t.Fatalf("want unknown category, got %v", err)
	}
}

func TestValidateNetwork(t *testing.T) {
	_, err := Validate([]Rule{{Name: "a", Action: ActionDirect, Domain: []string{"a.ru"}, Network: "tcp,udp"}}, testTags())
	if err == nil || fieldOf(t, err) != "rules[0].network" {
		t.Fatalf("want error on rules[0].network, got %v", err)
	}
}

func TestValidatePort(t *testing.T) {
	_, err := Validate([]Rule{{Name: "a", Action: ActionDirect, Port: "0"}}, testTags())
	if err == nil || fieldOf(t, err) != "rules[0].port" {
		t.Fatalf("want error on rules[0].port, got %v", err)
	}
}

func TestTagsMissing(t *testing.T) {
	tags := testTags()
	got := tags.Missing(Rule{Domain: []string{"geosite:google", "geosite:gone", "full:a.ru"}})
	if !slices.Equal(got, []string{"geosite:gone"}) {
		t.Fatalf("missing = %v", got)
	}
	if got := tags.Missing(Rule{IP: []string{"geoip:ru", "geoip:xx", "1.2.3.4"}}); !slices.Equal(got, []string{"geoip:xx"}) {
		t.Fatalf("missing ip = %v", got)
	}
	var none *Tags
	if none.Loaded() || none.Missing(Rule{Domain: []string{"geosite:x"}}) != nil {
		t.Fatal("nil tags must be not loaded and report nothing missing")
	}
}

func TestValidateIPv6(t *testing.T) {
	out, err := Validate([]Rule{{Name: "v6", Action: ActionDirect, IP: []string{"2001:DB8::1/32", "::1"}}}, testTags())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out[0].IP, []string{"2001:db8::/32", "::1"}) {
		t.Fatalf("ip = %v", out[0].IP)
	}
}

func TestValidatePortStrict(t *testing.T) {
	for _, p := range []string{"+443", "443-+500", " 443", "0443x"} {
		_, err := Validate([]Rule{{Name: "a", Action: ActionBlock, Port: p}}, testTags())
		if err == nil || fieldOf(t, err) != "rules[0].port" {
			t.Fatalf("%q: want error on rules[0].port, got %v", p, err)
		}
	}
	out, err := Validate([]Rule{{Name: "a", Action: ActionBlock, Port: "80,1000-2000"}}, testTags())
	if err != nil || out[0].Port != "80,1000-2000" {
		t.Fatalf("valid ports: %v %v", out, err)
	}
}

func TestValidateIPv6Zone(t *testing.T) {
	_, err := Validate([]Rule{{Name: "a", Action: ActionDirect, IP: []string{"fe80::1%eth0"}}}, testTags())
	if err == nil || fieldOf(t, err) != "rules[0].ip" {
		t.Fatalf("want error on rules[0].ip, got %v", err)
	}
}

func TestValidateSuffixAlias(t *testing.T) {
	out, err := Validate([]Rule{{Name: "a", Action: ActionDirect, Domain: []string{"suffix:Example.com", "SUFFIX:b.ru"}}}, testTags())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out[0].Domain, []string{"domain:example.com", "domain:b.ru"}) {
		t.Fatalf("domain = %v", out[0].Domain)
	}
	if _, err := Validate([]Rule{{Name: "a", Action: ActionDirect, Domain: []string{"suffix:"}}}, testTags()); err == nil || fieldOf(t, err) != "rules[0].domain" {
		t.Fatalf("empty suffix: %v", err)
	}
	if _, err := Validate([]Rule{{Name: "a", Action: ActionDirect, IP: []string{"suffix:a.ru"}}}, testTags()); err == nil || fieldOf(t, err) != "rules[0].ip" {
		t.Fatalf("suffix in ip: %v", err)
	}
}
