package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

var planRuleTag = regexp.MustCompile(`^sylph-p([0-9]+)-`)

type PlanRules struct {
	xray   XrayClient
	logger *slog.Logger
	gen    int64
	key    string
	err    string
}

func NewPlanRules(xray XrayClient, logger *slog.Logger) *PlanRules {
	return &PlanRules{xray: xray, logger: logger}
}

func (k *PlanRules) Error() *string {
	if k.err == "" {
		return nil
	}
	msg := k.err
	return &msg
}

func (k *PlanRules) Keep(ctx context.Context, profile *noderoute.Profile, users map[int64][]string) {
	if profile == nil || !noderoute.HasPlanRules(profile) {
		k.err, k.key = "", ""
		return
	}
	live, err := k.xray.RuleTags(ctx)
	if err != nil {
		k.fail("routing_rules_unreadable", err)
		return
	}
	content, _ := json.Marshal(noderoute.Tail(profile, users, 0))
	key := string(content)
	k.err = ""
	if key == k.key && k.gen > 0 {
		current := tagsOf(noderoute.Tail(profile, users, k.gen))
		if containsAll(live, current) {
			k.cleanup(ctx, live, current)
			return
		}
	}
	gen := k.gen
	for _, tag := range live {
		if m := planRuleTag.FindStringSubmatch(tag); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n > gen {
				gen = n
			}
		}
	}
	gen++
	next := noderoute.Tail(profile, users, gen)
	if err := k.xray.AppendRules(ctx, next); err != nil {
		k.fail("routing_rules_failed", err)
		return
	}
	k.gen, k.key = gen, key
	k.logger.Info("routing_plan_rules_applied", "generation", gen, "rules", len(next))
	k.cleanup(ctx, live, tagsOf(next))
}

func (k *PlanRules) cleanup(ctx context.Context, live, keep []string) {
	var defaults, rest []string
	for _, tag := range live {
		switch {
		case slices.Contains(keep, tag):
		case tag == noderoute.DefaultRuleTag:
			if slices.ContainsFunc(keep, func(t string) bool { return strings.HasSuffix(t, "-default") }) {
				defaults = append([]string{tag}, defaults...)
			}
		case planRuleTag.MatchString(tag) && strings.HasSuffix(tag, "-default"):
			defaults = append(defaults, tag)
		case planRuleTag.MatchString(tag):
			rest = append(rest, tag)
		}
	}
	for _, tag := range append(defaults, rest...) {
		if err := k.xray.RemoveRule(ctx, tag); err != nil {
			k.fail("routing_rules_failed", err)
		}
	}
}

func (k *PlanRules) fail(code string, err error) {
	k.err = code + ": " + truncate(err.Error(), 300)
	k.logger.Error(code, "error", k.err)
}

func containsAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func tagsOf(rules []noderoute.TailRule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Tag)
	}
	return out
}
