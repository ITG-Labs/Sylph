package agent

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

type Engine struct {
	cfg         *Config
	core        *CoreClient
	xray        XrayClient
	logger      *slog.Logger
	version     int64
	desired     map[string]bool
	blocked     map[string]bool
	haveDesired bool
	inbounds    []Inbound
	users       []SyncUser
	routing     *noderoute.Profile
	serviceKeys []string
	geo         *SyncGeo
}

type NodeDesired struct {
	Inbounds    []Inbound
	Routing     *noderoute.Profile
	ServiceKeys []string
	Geo         *SyncGeo
	Egress      []noderoute.Egress
}

func NewEngine(cfg *Config, core *CoreClient, xray XrayClient, logger *slog.Logger) *Engine {
	return &Engine{
		cfg:     cfg,
		core:    core,
		xray:    xray,
		logger:  logger,
		version: -1,
		desired: map[string]bool{},
	}
}

func (e *Engine) ActiveUsers() map[string]bool {
	out := make(map[string]bool, len(e.desired))
	for k, v := range e.desired {
		out[k] = v
	}
	return out
}

func (e *Engine) Block(emails map[string]bool) {
	e.blocked = make(map[string]bool, len(emails))
	for email := range emails {
		uuid, _ := splitEmail(email)
		e.blocked[uuid] = true
	}
}

func (e *Engine) Inbounds() []Inbound {
	return e.inbounds
}

func (e *Engine) setDesired(list []SyncUser) {
	e.users = list
	e.desired = make(map[string]bool, len(list)+len(e.serviceKeys))
	for _, u := range list {
		e.desired[u.UUID] = true
	}
	for _, k := range e.serviceKeys {
		e.desired[k] = true
	}
	e.haveDesired = true
}

func (e *Engine) setRouting(routing *noderoute.Profile, serviceKeys []string, geo *SyncGeo) {
	e.routing, e.serviceKeys, e.geo = routing, serviceKeys, geo
}

func (e *Engine) Desired(status []AddressStatus) NodeDesired {
	return NodeDesired{Inbounds: e.inbounds, Routing: e.routing, ServiceKeys: e.serviceKeys, Geo: e.geo,
		Egress: buildEgress(status, e.users, e.nodeEmails)}
}

func (e *Engine) nodeEmails(u SyncUser) []string {
	out := make([]string, 0, len(e.inbounds))
	for _, in := range e.inbounds {
		out = append(out, userEmail(u.UUID, in.Tag))
	}
	slices.Sort(out)
	return out
}

func (e *Engine) emailsOf(u SyncUser) []string {
	tags := u.Inbounds
	if len(tags) == 0 {
		for _, in := range e.inbounds {
			tags = append(tags, in.Tag)
		}
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		out = append(out, userEmail(u.UUID, tag))
	}
	slices.Sort(out)
	return out
}

func (e *Engine) desiredOn(tag string) map[string]string {
	out := map[string]string{}
	for _, u := range e.users {
		if len(u.Inbounds) == 0 || slices.Contains(u.Inbounds, tag) {
			out[userEmail(u.UUID, tag)] = u.UUID
		}
	}
	for _, k := range e.serviceKeys {
		out[k] = k
	}
	return out
}

func (e *Engine) PlanUsers() map[int64][]string {
	out := map[int64][]string{}
	for _, u := range e.users {
		if u.PlanID != 0 {
			out[u.PlanID] = append(out[u.PlanID], e.emailsOf(u)...)
		}
	}
	for plan := range out {
		slices.Sort(out[plan])
	}
	return out
}

func (e *Engine) LoadState(path string) error {
	c, err := LoadCache(path)
	if err != nil {
		return err
	}
	if c.Version < 0 {
		e.logger.Info("cache_absent")
		return nil
	}
	e.setRouting(c.Routing, c.ServiceKeys, c.Geo)
	e.setDesired(c.Users)
	e.inbounds = c.Inbounds
	e.version = c.Version
	if c.Inbounds == nil || !c.PerInbound {
		e.version = -1
	}
	e.logger.Info("cache_loaded", "version", c.Version, "users", len(c.Users), "inbounds", len(c.Inbounds), "inbounds_known", c.Inbounds != nil)
	return nil
}

func (e *Engine) SyncOnce(ctx context.Context) (*SyncResp, error) {
	resp, err := e.core.SyncUsers(ctx, e.version)
	if err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}
	if resp.Users == nil {
		return resp, nil
	}
	e.setRouting(resp.Routing, resp.ServiceKeys, resp.Geo)
	e.setDesired(resp.Users)
	e.inbounds = resp.Inbounds
	e.version = resp.Version
	if err := SaveCache(e.cfg.CachePath, &Cache{Version: resp.Version, PerInbound: true, Users: resp.Users, Inbounds: resp.Inbounds,
		Routing: resp.Routing, ServiceKeys: resp.ServiceKeys, Geo: resp.Geo}); err != nil {
		e.logger.Error("cache_save_failed", "error", err.Error())
	}
	e.logger.Info("sync_received", "version", resp.Version, "users", len(resp.Users), "inbounds", len(resp.Inbounds))
	return resp, nil
}

func (e *Engine) Reconcile(ctx context.Context, inbounds []Inbound) error {
	return e.reconcileAll(ctx, inbounds, true)
}

func (e *Engine) Revoke(ctx context.Context, tags []string) error {
	inbounds := make([]Inbound, 0, len(tags))
	for _, tag := range tags {
		inbounds = append(inbounds, Inbound{Tag: tag})
	}
	return e.reconcileAll(ctx, inbounds, false)
}

func (e *Engine) reconcileAll(ctx context.Context, inbounds []Inbound, add bool) error {
	if !e.haveDesired {
		return nil
	}
	var firstErr error
	for _, in := range inbounds {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.reconcileInbound(ctx, in, add); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (e *Engine) reconcileInbound(ctx context.Context, in Inbound, add bool) error {
	live, err := e.xray.ListUsers(ctx, in.Tag)
	if err != nil {
		return fmt.Errorf("list xray users in %s: %w", in.Tag, err)
	}
	actual := make(map[string]bool, len(live))
	for _, email := range live {
		actual[email] = true
	}
	want := e.desiredOn(in.Tag)
	keys := make(map[string]bool, len(want))
	for _, uuid := range want {
		keys[uuid] = true
	}
	var firstErr error
	for email := range actual {
		uuid, wanted := want[email]
		if legacy, tag := splitEmail(email); !add && tag == "" && keys[legacy] {
			uuid, wanted = legacy, true
		}
		if wanted && !e.blocked[uuid] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		uuid, _ = splitEmail(email)
		if err := e.xray.RemoveUser(ctx, in.Tag, email); err != nil {
			firstErr = e.recordFailure(firstErr, "remove", in.Tag, uuid, err)
			continue
		}
		delete(actual, email)
		e.logger.Info("xray_user_removed", "inbound", in.Tag, "key_ref", keyRef(uuid))
	}
	if !add {
		return firstErr
	}
	for email, uuid := range want {
		if actual[email] || e.blocked[uuid] || (email != uuid && actual[uuid]) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.xray.AddUser(ctx, in.Tag, uuid, email, in.Flow); err != nil {
			firstErr = e.recordFailure(firstErr, "add", in.Tag, uuid, err)
			continue
		}
		e.logger.Info("xray_user_added", "inbound", in.Tag, "key_ref", keyRef(uuid))
	}
	return firstErr
}

func (e *Engine) recordFailure(firstErr error, op string, tag string, uuid string, err error) error {
	msg := redactKey(err.Error(), uuid)
	e.logger.Error("xray_"+op+"_failed", "inbound", tag, "key_ref", keyRef(uuid), "error", msg)
	if firstErr != nil {
		return firstErr
	}
	return fmt.Errorf("%s user %s in %s: %s", op, keyRef(uuid), tag, msg)
}
