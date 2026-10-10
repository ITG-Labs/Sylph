package agent

import (
	"context"
	"fmt"
	"log/slog"
)

type Engine struct {
	cfg     *Config
	core    *CoreClient
	xray    XrayClient
	logger  *slog.Logger
	version int64
	users   map[string]bool
}

func NewEngine(cfg *Config, core *CoreClient, xray XrayClient, logger *slog.Logger) *Engine {
	return &Engine{
		cfg:    cfg,
		core:   core,
		xray:   xray,
		logger: logger,
		users:  map[string]bool{},
	}
}

func (e *Engine) CurrentVersion() int64 {
	return e.version
}

func (e *Engine) ActiveUsers() map[string]bool {
	out := make(map[string]bool, len(e.users))
	for k, v := range e.users {
		out[k] = v
	}
	return out
}

func (e *Engine) LoadState(path string) error {
	c, err := LoadCache(path)
	if err != nil {
		return err
	}
	if err := e.applyDiff(context.Background(), c.Users); err != nil {
		return fmt.Errorf("apply cached state: %w", err)
	}
	e.version = c.Version
	e.logger.Info("cache_loaded", "version", c.Version, "users", len(c.Users))
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
	if err := e.applyDiff(ctx, resp.Users); err != nil {
		return resp, fmt.Errorf("apply: %w", err)
	}
	e.version = resp.Version
	if err := SaveCache(e.cfg.CachePath, &Cache{Version: resp.Version, Users: resp.Users, Config: resp.Config}); err != nil {
		e.logger.Error("cache_save_failed", "error", err.Error())
	}
	e.logger.Info("sync_applied", "version", resp.Version, "users", len(resp.Users))
	return resp, nil
}

func (e *Engine) applyDiff(ctx context.Context, want []SyncUser) error {
	wantSet := make(map[string]bool, len(want))
	for _, u := range want {
		wantSet[u.UUID] = true
	}
	var firstErr error
	for uuid := range e.users {
		if wantSet[uuid] {
			continue
		}
		if err := e.xray.RemoveUser(ctx, e.cfg.InboundTag, uuid); err != nil {
			msg := redactKey(err.Error(), uuid)
			e.logger.Error("xray_remove_failed", "key_ref", keyRef(uuid), "error", msg)
			if firstErr == nil {
				firstErr = fmt.Errorf("remove user %s: %s", keyRef(uuid), msg)
			}
			continue
		}
		delete(e.users, uuid)
	}
	for uuid := range wantSet {
		if e.users[uuid] {
			continue
		}
		if err := e.xray.AddUser(ctx, e.cfg.InboundTag, uuid); err != nil {
			msg := redactKey(err.Error(), uuid)
			e.logger.Error("xray_add_failed", "key_ref", keyRef(uuid), "error", msg)
			if firstErr == nil {
				firstErr = fmt.Errorf("add user %s: %s", keyRef(uuid), msg)
			}
			continue
		}
		e.users[uuid] = true
	}
	return firstErr
}
