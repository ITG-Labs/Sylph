package agent

import (
	"context"
	"fmt"
	"log/slog"
)

type Engine struct {
	cfg         *Config
	core        *CoreClient
	xray        XrayClient
	logger      *slog.Logger
	version     int64
	desired     map[string]bool
	haveDesired bool
	inbounds    []Inbound
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

func (e *Engine) Inbounds() []Inbound {
	return e.inbounds
}

func (e *Engine) setDesired(list []SyncUser) {
	e.desired = make(map[string]bool, len(list))
	for _, u := range list {
		e.desired[u.UUID] = true
	}
	e.haveDesired = true
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
	e.setDesired(c.Users)
	e.inbounds = c.Inbounds
	e.version = c.Version
	if c.Inbounds == nil {
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
	e.setDesired(resp.Users)
	e.inbounds = resp.Inbounds
	e.version = resp.Version
	if err := SaveCache(e.cfg.CachePath, &Cache{Version: resp.Version, Users: resp.Users, Inbounds: resp.Inbounds}); err != nil {
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
	for _, uuid := range live {
		actual[uuid] = true
	}
	var firstErr error
	for uuid := range actual {
		if e.desired[uuid] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.xray.RemoveUser(ctx, in.Tag, uuid); err != nil {
			firstErr = e.recordFailure(firstErr, "remove", in.Tag, uuid, err)
			continue
		}
		e.logger.Info("xray_user_removed", "inbound", in.Tag, "key_ref", keyRef(uuid))
	}
	if !add {
		return firstErr
	}
	for uuid := range e.desired {
		if actual[uuid] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.xray.AddUser(ctx, in.Tag, uuid, in.Flow); err != nil {
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
