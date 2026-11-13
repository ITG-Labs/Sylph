package clientroute

import (
	"slices"
	"testing"
)

func issueFor(issues []Issue, c Client) (Issue, bool) {
	for _, i := range issues {
		if i.Client == c {
			return i, true
		}
	}
	return Issue{}, false
}

func TestIssuesPort(t *testing.T) {
	issues := Issues(Rule{Name: "smtp", Action: ActionBlock, Port: "25", Domain: []string{"full:smtp.x.ru"}})
	if got, _ := issueFor(issues, Happ); got != (Issue{Client: Happ, Skip: true, Reason: "port"}) {
		t.Fatalf("happ = %+v", got)
	}
	if got, _ := issueFor(issues, SimpleXray); got != (Issue{Client: SimpleXray, Skip: true, Reason: "port"}) {
		t.Fatalf("simplexray = %+v", got)
	}
	if _, ok := issueFor(issues, ITGRay); ok {
		t.Fatal("itgray must have no issue")
	}
}

func TestIssuesNetwork(t *testing.T) {
	issues := Issues(Rule{Action: ActionDirect, Network: "udp", Domain: []string{"a.ru"}})
	if got, _ := issueFor(issues, Happ); got.Reason != "network" || !got.Skip {
		t.Fatalf("happ = %+v", got)
	}
}

func TestIssuesKeywordOnlySimpleXray(t *testing.T) {
	for _, v := range []string{"keyword:ads", "regexp:^ads\\."} {
		issues := Issues(Rule{Action: ActionBlock, Domain: []string{v}})
		if len(issues) != 1 {
			t.Fatalf("%s: issues = %+v", v, issues)
		}
		want := "keyword"
		if v[0] == 'r' {
			want = "regexp"
		}
		if issues[0] != (Issue{Client: SimpleXray, Skip: true, Reason: want}) {
			t.Fatalf("%s: issue = %+v", v, issues[0])
		}
	}
}

func TestIssuesNodeHapp(t *testing.T) {
	issues := Issues(Rule{Action: ActionNode, NodeID: 1, Domain: []string{"openai.com"}})
	if len(issues) != 1 || issues[0] != (Issue{Client: Happ, Skip: false, Reason: "node_via_active"}) {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestIssuesPortWinsOverNode(t *testing.T) {
	issues := Issues(Rule{Action: ActionNode, NodeID: 1, Port: "443"})
	if got, _ := issueFor(issues, Happ); got.Reason != "port" || !got.Skip {
		t.Fatalf("happ = %+v", got)
	}
}

func TestIssuesCategoryFormat(t *testing.T) {
	issues := Issues(Rule{Action: ActionDirect, Domain: []string{"geosite:a.b"}})
	if len(issues) != 1 || issues[0] != (Issue{Client: SimpleXray, Skip: true, Reason: "category_format"}) {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestIssuesNone(t *testing.T) {
	if issues := Issues(Rule{Action: ActionDirect, IP: []string{"geoip:ru", "10.0.0.0/8"}}); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestOverlaps(t *testing.T) {
	got := Overlaps([]Rule{
		{Action: ActionDirect, Domain: []string{"geosite:google"}},
		{Action: ActionBlock, Domain: []string{"geosite:google"}},
	})
	if !slices.Equal(got, []Overlap{{Rule: 1, Earlier: 0, Value: "geosite:google"}}) {
		t.Fatalf("overlaps = %+v", got)
	}
	if got := Overlaps([]Rule{
		{Action: ActionProxy, Domain: []string{"domain:x.ru"}},
		{Action: ActionNode, NodeID: 3, Domain: []string{"domain:x.ru"}},
	}); len(got) != 0 {
		t.Fatalf("proxy and node are one action: %+v", got)
	}
	if got := Overlaps([]Rule{
		{Action: ActionDirect, Port: "25"},
		{Action: ActionBlock, Port: "25"},
	}); !slices.Equal(got, []Overlap{{Rule: 1, Earlier: 0, Value: "port:25"}}) {
		t.Fatalf("port overlaps = %+v", got)
	}
	if got := Overlaps([]Rule{
		{Action: ActionDirect, Domain: []string{"geosite:a"}},
		{Action: ActionBlock, Domain: []string{"geosite:a"}},
		{Action: ActionBlock, Domain: []string{"geosite:a"}},
	}); !slices.Equal(got, []Overlap{{Rule: 1, Earlier: 0, Value: "geosite:a"}, {Rule: 2, Earlier: 0, Value: "geosite:a"}}) {
		t.Fatalf("nearest different-action earlier rule: %+v", got)
	}
	if got := Overlaps([]Rule{
		{Action: ActionDirect, Domain: []string{"geosite:a"}},
		{Action: ActionProxy, Domain: []string{"geosite:a"}},
		{Action: ActionBlock, Domain: []string{"geosite:a"}},
	}); !slices.Equal(got, []Overlap{{Rule: 1, Earlier: 0, Value: "geosite:a"}, {Rule: 2, Earlier: 1, Value: "geosite:a"}}) {
		t.Fatalf("only the nearest earlier rule is reported: %+v", got)
	}
}
