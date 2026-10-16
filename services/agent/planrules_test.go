package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

func planProfile(def string) *noderoute.Profile {
	p := routedProfile()
	p.Rules = []noderoute.Rule{
		{Protocol: []string{"bittorrent"}, To: "block"},
		{Plans: []int64{3}, To: "nl"},
		{Plans: []int64{4}, To: "best"},
	}
	p.Default = def
	return p
}

func ruleByTag(rules []noderoute.TailRule, tag string) noderoute.TailRule {
	i := slices.IndexFunc(rules, func(r noderoute.TailRule) bool { return r.Tag == tag })
	if i < 0 {
		return noderoute.TailRule{}
	}
	return rules[i]
}

func routedRig(t *testing.T, p *noderoute.Profile) *nodeRig {
	t.Helper()
	r := newNodeRig(t, newFakeXray())
	r.node.ApplyDesired(context.Background(), NodeDesired{Inbounds: mainIn, Routing: p})
	if !r.node.Settled() {
		t.Fatalf("routed config not applied: %v", r.node.ConfigError())
	}
	return r
}

func TestPlanRulesFollowPlanUsersWithoutRestart(t *testing.T) {
	ctx := context.Background()
	p := planProfile("direct")
	r := routedRig(t, p)
	kills := r.proc.killCount()
	k := NewPlanRules(r.fx, discardLogger())
	k.Keep(ctx, p, map[int64][]string{3: {"a"}})
	static := []string{noderoute.GuardRuleTag, noderoute.GuardNamesRuleTag, "sylph-r1"}
	live := r.fx.liveRules()
	if got := tagsOf(live); !slices.Equal(got, append(slices.Clone(static), "sylph-p1-r2")) {
		t.Fatalf("live rules = %v", got)
	}
	if rule := ruleByTag(live, "sylph-p1-r2"); !slices.Equal(rule.Users, []string{"a"}) || rule.OutboundTag != "nl" {
		t.Fatalf("plan rule = %+v", rule)
	}
	calls := r.fx.routingCalls()
	k.Keep(ctx, p, map[int64][]string{3: {"a"}})
	if r.fx.routingCalls() != calls+1 {
		t.Fatalf("unchanged plan users touched xray rules: %d calls", r.fx.routingCalls()-calls)
	}
	k.Keep(ctx, p, map[int64][]string{3: {"b", "a"}, 4: {"c"}})
	live = r.fx.liveRules()
	if got := tagsOf(live); !slices.Equal(got, append(slices.Clone(static), "sylph-p2-r2", "sylph-p2-r3")) {
		t.Fatalf("live rules after a payment = %v", got)
	}
	if rule := ruleByTag(live, "sylph-p2-r2"); !slices.Equal(rule.Users, []string{"a", "b"}) {
		t.Fatalf("plan rule = %+v", rule)
	}
	if rule := ruleByTag(live, "sylph-p2-r3"); rule.BalancerTag != "best" || !slices.Equal(rule.Users, []string{"c"}) {
		t.Fatalf("balancer plan rule = %+v", rule)
	}
	if r.proc.killCount() != kills || k.Error() != nil {
		t.Fatalf("kills %d, error %v", r.proc.killCount()-kills, k.Error())
	}
	pid, _ := r.proc.Find()
	if err := r.proc.Terminate(pid); err != nil {
		t.Fatal(err)
	}
	k.Keep(ctx, p, map[int64][]string{3: {"b", "a"}, 4: {"c"}})
	if got := tagsOf(r.fx.liveRules()); !slices.Equal(got, append(slices.Clone(static), "sylph-p3-r2", "sylph-p3-r3")) {
		t.Fatalf("rules not restored after an xray restart: %v", got)
	}
	again := NewPlanRules(r.fx, discardLogger())
	again.Keep(ctx, p, map[int64][]string{3: {"a"}})
	if got := tagsOf(r.fx.liveRules()); again.Error() != nil || !slices.Equal(got, append(slices.Clone(static), "sylph-p4-r2")) {
		t.Fatalf("a restarted agent clashed with the rules already in xray: %v %v", again.Error(), got)
	}
}

func TestPlanRulesTakeOverTheBalancerDefault(t *testing.T) {
	ctx := context.Background()
	p := planProfile("best")
	r := routedRig(t, p)
	static := []string{noderoute.GuardRuleTag, noderoute.GuardNamesRuleTag, "sylph-r1"}
	if got := tagsOf(r.fx.liveRules()); !slices.Equal(got, append(slices.Clone(static), noderoute.DefaultRuleTag)) {
		t.Fatalf("file rules = %v", got)
	}
	k := NewPlanRules(r.fx, discardLogger())
	k.Keep(ctx, p, map[int64][]string{3: {"a"}})
	live := r.fx.liveRules()
	if got := tagsOf(live); !slices.Equal(got, append(slices.Clone(static), "sylph-p1-r2", "sylph-p1-default")) {
		t.Fatalf("live rules = %v", got)
	}
	if rule := ruleByTag(live, "sylph-p1-default"); !rule.CatchAll || rule.BalancerTag != "best" {
		t.Fatalf("catch-all = %+v", rule)
	}
	k.Keep(ctx, p, map[int64][]string{})
	if got := tagsOf(r.fx.liveRules()); !slices.Equal(got, append(slices.Clone(static), "sylph-p2-default")) {
		t.Fatalf("an empty plan lost the balancer default: %v", got)
	}
}

func TestPlanRulesSkipProfilesWithoutThemAndReportErrors(t *testing.T) {
	ctx := context.Background()
	r := newNodeRig(t, newFakeXray())
	k := NewPlanRules(r.fx, discardLogger())
	k.Keep(ctx, nil, map[int64][]string{3: {"a"}})
	plain := routedProfile()
	plain.Rules = plain.Rules[:2]
	k.Keep(ctx, plain, map[int64][]string{3: {"a"}})
	if r.fx.routingCalls() != 0 || k.Error() != nil {
		t.Fatalf("routing API used without plan rules: %d calls, error %v", r.fx.routingCalls(), k.Error())
	}
	p := planProfile("direct")
	r = routedRig(t, p)
	k = NewPlanRules(r.fx, discardLogger())
	r.fx.set(func(f *fakeXray) { f.ruleErr = errors.New("routing service is off") })
	k.Keep(ctx, p, map[int64][]string{3: {"a"}})
	if e := k.Error(); e == nil || !strings.Contains(*e, "routing service is off") {
		t.Fatalf("routing_error = %v", e)
	}
	k.Keep(ctx, nil, nil)
	if k.Error() != nil {
		t.Fatal("error kept after the profile was removed")
	}
	r.fx.set(func(f *fakeXray) { f.ruleErr = nil })
	k.Keep(ctx, p, map[int64][]string{3: {"a"}})
	if k.Error() != nil || !slices.Contains(tagsOf(r.fx.liveRules()), "sylph-p1-r2") {
		t.Fatalf("did not recover: %v %v", k.Error(), tagsOf(r.fx.liveRules()))
	}
}
