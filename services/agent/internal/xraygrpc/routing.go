package xraygrpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/router"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/routercmd"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/xnet"
)

type RouteRule = noderoute.TailRule

func (c *Client) AppendRules(ctx context.Context, rules []RouteRule) error {
	if len(rules) == 0 {
		return nil
	}
	cfg := &router.Config{}
	for _, r := range rules {
		rule := &router.RoutingRule{RuleTag: r.Tag}
		if r.CatchAll {
			rule.Networks = []xnet.Network{xnet.Network_TCP, xnet.Network_UDP}
		} else {
			if len(r.Users) == 0 {
				return fmt.Errorf("xray rule %s: no users", r.Tag)
			}
			rule.UserEmail = r.Users
		}
		if r.BalancerTag != "" {
			rule.TargetTag = &router.RoutingRule_BalancingTag{BalancingTag: r.BalancerTag}
		} else {
			rule.TargetTag = &router.RoutingRule_Tag{Tag: r.OutboundTag}
		}
		cfg.Rule = append(cfg.Rule, rule)
	}
	msg, err := typed(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if _, err := c.routing.AddRule(ctx, &routercmd.AddRuleRequest{Config: msg, ShouldAppend: true}); err != nil {
		return fmt.Errorf("xray add rules: %w", err)
	}
	return nil
}

func (c *Client) RemoveRule(ctx context.Context, tag string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	if _, err := c.routing.RemoveRule(ctx, &routercmd.RemoveRuleRequest{RuleTag: tag}); err != nil {
		return fmt.Errorf("xray remove rule %s: %w", tag, err)
	}
	return nil
}

func (c *Client) RuleTags(ctx context.Context) ([]string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resp, err := c.routing.ListRule(ctx, &routercmd.ListRuleRequest{})
	if err != nil {
		return nil, fmt.Errorf("xray list rules: %w", err)
	}
	tags := make([]string, 0, len(resp.GetRules()))
	for _, r := range resp.GetRules() {
		tags = append(tags, r.GetRuleTag())
	}
	return tags, nil
}

func (c *Client) TestRoute(ctx context.Context, inboundTag, user string) (string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	resp, err := c.routing.TestRoute(ctx, &routercmd.TestRouteRequest{RoutingContext: &routercmd.RoutingContext{
		InboundTag: inboundTag, User: user, Network: xnet.Network_TCP, TargetDomain: "example.com", TargetPort: 443,
	}})
	if err != nil {
		if strings.Contains(err.Error(), "not enough information") {
			return "", nil
		}
		return "", fmt.Errorf("xray test route: %w", err)
	}
	return resp.GetOutboundTag(), nil
}
