package agent

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/ITG-Labs/sylph/internal/version"
)

const (
	maxOnlineIPsPerKey = 32
	maxOnlineReport    = 20000
)

type Loop struct {
	engine  *Engine
	stats   *StatsCollector
	core    *CoreClient
	xray    XrayClient
	node    *NodeConfig
	rules   *PlanRules
	cfg     *Config
	logger  *slog.Logger
	started time.Time
	applied []Inbound
	egress  []AddressStatus
	checker *EgressChecker
	held    bool
}

func NewLoop(e *Engine, sc *StatsCollector, core *CoreClient, xray XrayClient, node *NodeConfig, cfg *Config, logger *slog.Logger) *Loop {
	return &Loop{engine: e, stats: sc, core: core, xray: xray, node: node, rules: NewPlanRules(xray, logger), cfg: cfg, logger: logger, started: time.Now(),
		checker: NewEgressChecker(cfg.Egress)}
}

func (l *Loop) RunOnce(ctx context.Context) error {
	l.reportTraffic(ctx)
	_, syncErr := l.engine.SyncOnce(ctx)
	if syncErr != nil {
		l.logger.Error("sync_failed", "error", syncErr.Error())
	}
	applyErr := l.apply(ctx)
	if applyErr != nil {
		l.logger.Error("reconcile_failed", "error", applyErr.Error())
	}
	if err := l.core.Heartbeat(ctx, l.heartbeat(ctx)); err != nil {
		l.logger.Error("heartbeat_failed", "error", err.Error())
	}
	return errors.Join(syncErr, applyErr)
}

func (l *Loop) heartbeat(ctx context.Context) Heartbeat {
	hb := Heartbeat{
		AgentVersion:   version.Version,
		XrayVersion:    l.cfg.XrayVersion,
		AgentUptimeSec: int64(time.Since(l.started) / time.Second),
		ConfigError:    l.node.ConfigError(),
		RoutingError:   l.rules.Error(),
		Addresses:      l.egress,
	}
	if l.node.geo != nil {
		if st := l.node.geo.Status(); len(st) > 0 {
			hb.GeoStatus = st
		}
	}
	if count, ok := l.activeUsers(ctx, l.applied); ok {
		hb.ActiveUsers = &count
	}
	if online, ok := l.online(ctx); ok {
		hb.Online = &online
	}
	if uptime, err := l.xray.Uptime(ctx); err != nil {
		l.logger.Error("heartbeat_xray_probe_failed", "probe", "uptime", "error", err.Error())
	} else {
		sec := int64(uptime / time.Second)
		hb.XrayUptimeSec = &sec
	}
	return hb
}

func (l *Loop) online(ctx context.Context) ([]OnlineKey, bool) {
	raw, err := l.xray.OnlineIPs(ctx)
	if err != nil {
		l.logger.Error("heartbeat_xray_probe_failed", "probe", "online", "error", err.Error())
		return nil, false
	}
	now := time.Now().Unix()
	out := make([]OnlineKey, 0, len(raw))
	total := 0
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		ips := slices.Sorted(maps.Keys(raw[key]))
		if len(ips) > maxOnlineIPsPerKey {
			ips = ips[:maxOnlineIPsPerKey]
		}
		if total+len(ips) > maxOnlineReport {
			l.logger.Warn("heartbeat_online_truncated", "keys", len(raw))
			break
		}
		k := OnlineKey{Key: key, IPs: make([]OnlineIP, len(ips))}
		for i, ip := range ips {
			k.IPs[i] = OnlineIP{IP: ip, AgeSec: max(0, now-raw[key][ip])}
		}
		total += len(ips)
		out = append(out, k)
	}
	return out, true
}

func (l *Loop) activeUsers(ctx context.Context, inbounds []Inbound) (int, bool) {
	if inbounds == nil {
		return 0, false
	}
	seen := map[string]bool{}
	for _, in := range inbounds {
		users, err := l.xray.ListUsers(ctx, in.Tag)
		if err != nil {
			l.logger.Error("heartbeat_xray_probe_failed", "probe", "list_users", "inbound", in.Tag, "error", err.Error())
			return 0, false
		}
		for _, u := range users {
			seen[u] = true
		}
	}
	return len(seen), true
}

func (l *Loop) apply(ctx context.Context) error {
	l.egress = l.checker.Status(ctx)
	for _, st := range l.egress {
		if !st.OK {
			l.logger.Error("egress_address_unavailable", "ip", st.IP, "error", st.Error)
		}
	}
	want := l.engine.Desired(l.egress)
	d, held := l.node.HoldEgress(want, egressCooldown)
	if held && !l.held {
		l.logger.Info("egress_change_deferred", "cooldown", egressCooldown.String())
	}
	l.held = held
	if l.node.geo != nil {
		var need map[string][]string
		if d.Routing != nil {
			need = d.Routing.SourceCodes()
		}
		l.node.geo.Start(ctx, d.Geo, need)
	}
	l.applied = l.node.ApplyDesired(ctx, d)
	blocked := egressBlocked(l.node.AppliedEgress(), want.Egress)
	if len(blocked) > 0 {
		l.logger.Warn("egress_users_held_out", "users", len(blocked))
	}
	l.engine.Block(blocked)
	if l.applied == nil {
		return l.engine.Revoke(ctx, l.node.RunningTags())
	}
	err := l.engine.Reconcile(ctx, l.applied)
	if ctx.Err() == nil {
		l.rules.Keep(ctx, l.node.AppliedRouting(), l.engine.PlanUsers())
	}
	return err
}

func (l *Loop) ApplyCached(ctx context.Context) {
	if err := l.apply(ctx); err != nil {
		l.logger.Error("cache_apply_failed", "error", err.Error())
	}
}

func (l *Loop) reportTraffic(ctx context.Context) {
	batch, err := l.stats.Collect(ctx)
	if err != nil {
		l.logger.Error("stats_failed", "error", err.Error())
		return
	}
	if batch == nil {
		return
	}
	duplicate, err := l.core.ReportDeltas(ctx, batch)
	if err != nil {
		l.logger.Error("deltas_report_failed", "error", err.Error(), "batch_id", batch.ID, "count", len(batch.Deltas))
		return
	}
	l.stats.Commit(batch.ID)
	l.logger.Info("deltas_reported", "batch_id", batch.ID, "count", len(batch.Deltas), "duplicate", duplicate)
}

func (l *Loop) Run(ctx context.Context) error {
	ticker := time.NewTicker(l.cfg.SyncInterval)
	defer ticker.Stop()
	for {
		_ = l.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
