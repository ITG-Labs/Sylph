package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

type fakeXray struct {
	mu          sync.Mutex
	users       map[string]bool
	flows       map[string]string
	stats       map[string]int64
	addErr      error
	failAdd     map[string]bool
	failRemove  map[string]bool
	afterAdd    func()
	afterRemove func()
	listErr     error
	uptime      time.Duration
	uptimeErr   error
	uptimeFail  int
	uptimeHits  int
	rules       []noderoute.TailRule
	ruleErr     error
	ruleCalls   int
	online      map[string]map[string]int64
	onlineErr   error
}

func newFakeXray() *fakeXray {
	return &fakeXray{users: map[string]bool{}, flows: map[string]string{}, stats: map[string]int64{}}
}

func (f *fakeXray) AddUser(ctx context.Context, tag string, uuid string, flow string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	if f.failAdd[uuid] {
		return errors.New("add failed for " + uuid)
	}
	f.users[tag+"/"+uuid] = true
	f.flows[tag+"/"+uuid] = flow
	if f.afterAdd != nil {
		f.afterAdd()
	}
	return nil
}

func (f *fakeXray) RemoveUser(ctx context.Context, tag string, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRemove[uuid] {
		return errors.New("remove failed for " + uuid)
	}
	delete(f.users, tag+"/"+uuid)
	if f.afterRemove != nil {
		f.afterRemove()
	}
	return nil
}

func (f *fakeXray) ListUsers(ctx context.Context, tag string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []string
	for k := range f.users {
		if strings.HasPrefix(k, tag+"/") {
			out = append(out, strings.TrimPrefix(k, tag+"/"))
		}
	}
	return out, nil
}

func (f *fakeXray) wipe() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users = map[string]bool{}
	f.stats = map[string]int64{}
}

func (f *fakeXray) set(field func(*fakeXray)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	field(f)
}

func (f *fakeXray) QueryStats(ctx context.Context, pattern string) (map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int64{}
	for k, v := range f.stats {
		if strings.HasPrefix(k, pattern) {
			out[k] = v
		}
	}
	return out, nil
}

func (f *fakeXray) setStats(stats map[string]int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats = stats
}

func (f *fakeXray) hasUser(tag, uuid string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[tag+"/"+uuid]
}

func (f *fakeXray) flowOf(tag, uuid string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flows[tag+"/"+uuid]
}

func (f *fakeXray) userCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.users)
}

func (f *fakeXray) OnlineIPs(ctx context.Context) (map[string]map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onlineErr != nil {
		return nil, f.onlineErr
	}
	return f.online, nil
}

func (f *fakeXray) Uptime(ctx context.Context) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uptimeHits++
	if f.uptimeFail > 0 {
		f.uptimeFail--
		return 0, errors.New("xray is starting")
	}
	if f.uptimeErr != nil {
		return 0, f.uptimeErr
	}
	return f.uptime, nil
}

func (f *fakeXray) uptimeCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uptimeHits
}

func (f *fakeXray) AppendRules(ctx context.Context, rules []noderoute.TailRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ruleCalls++
	if f.ruleErr != nil {
		return f.ruleErr
	}
	for _, r := range rules {
		if slices.ContainsFunc(f.rules, func(x noderoute.TailRule) bool { return x.Tag == r.Tag }) {
			return fmt.Errorf("duplicate rule tag %s", r.Tag)
		}
	}
	f.rules = append(f.rules, rules...)
	return nil
}

func (f *fakeXray) RemoveRule(ctx context.Context, tag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ruleCalls++
	if f.ruleErr != nil {
		return f.ruleErr
	}
	i := slices.IndexFunc(f.rules, func(x noderoute.TailRule) bool { return x.Tag == tag })
	if i < 0 {
		return fmt.Errorf("rule %s not found", tag)
	}
	f.rules = slices.Delete(f.rules, i, i+1)
	return nil
}

func (f *fakeXray) RuleTags(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ruleCalls++
	if f.ruleErr != nil {
		return nil, f.ruleErr
	}
	tags := make([]string, 0, len(f.rules))
	for _, r := range f.rules {
		tags = append(tags, r.Tag)
	}
	return tags, nil
}

func (f *fakeXray) liveRules() []noderoute.TailRule {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.rules)
}

func (f *fakeXray) routingCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ruleCalls
}

func staticRules(config []byte) []noderoute.TailRule {
	var cfg struct {
		Routing struct {
			Rules []struct {
				RuleTag string `json:"ruleTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	_ = json.Unmarshal(config, &cfg)
	var out []noderoute.TailRule
	for _, r := range cfg.Routing.Rules {
		out = append(out, noderoute.TailRule{Tag: r.RuleTag})
	}
	return out
}
