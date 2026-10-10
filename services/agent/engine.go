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
	e.version = c.Version
	e.logger.Info("cache_loaded", "version", c.Version, "users", len(c.Users))
	if err := e.Reconcile(context.Background()); err != nil {
		return fmt.Errorf("apply cached state: %w", err)
	}
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
	e.version = resp.Version
	if err := SaveCache(e.cfg.CachePath, &Cache{Version: resp.Version, Users: resp.Users, Config: resp.Config}); err != nil {
		e.logger.Error("cache_save_failed", "error", err.Error())
	}
	e.logger.Info("sync_received", "version", resp.Version, "users", len(resp.Users))
	return resp, nil
}

func (e *Engine) Reconcile(ctx context.Context) error {
	if !e.haveDesired {
		return nil
	}
	live, err := e.xray.ListUsers(ctx, e.cfg.InboundTag)
	if err != nil {
		return fmt.Errorf("list xray users: %w", err)
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
		if err := e.xray.RemoveUser(ctx, e.cfg.InboundTag, uuid); err != nil {
			firstErr = e.recordFailure(firstErr, "remove", uuid, err)
			continue
		}
		e.logger.Info("xray_user_removed", "key_ref", keyRef(uuid))
	}
	for uuid := range e.desired {
		if actual[uuid] {
			continue
		}
		if err := e.xray.AddUser(ctx, e.cfg.InboundTag, uuid); err != nil {
			firstErr = e.recordFailure(firstErr, "add", uuid, err)
			continue
		}
		e.logger.Info("xray_user_added", "key_ref", keyRef(uuid))
	}
	return firstErr
}

func (e *Engine) recordFailure(firstErr error, op string, uuid string, err error) error {
	msg := redactKey(err.Error(), uuid)
	e.logger.Error("xray_"+op+"_failed", "key_ref", keyRef(uuid), "error", msg)
	if firstErr != nil {
		return firstErr
	}
	return fmt.Errorf("%s user %s: %s", op, keyRef(uuid), msg)
}
