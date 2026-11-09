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
	xrayRestartSlack   = 2 * time.Second
	xrayRefillGap      = 10 * time.Second
	maxOnlineIPsPerKey = 32
	maxOnlineReport    = 20000
	maxOnlineAgeSec    = 30 * 24 * 3600
	maxOnlineWalk      = 10 * time.Second
)

func onlineBudget(cfg *Config) time.Duration {
	if half := cfg.SyncInterval / 2; half > 0 && half < maxOnlineWalk {
		return half
	}
	return maxOnlineWalk
}

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

	xrayStart  time.Time
	lastRefill time.Time
	refillGap  time.Duration
}

func NewLoop(e *Engine, sc *StatsCollector, core *CoreClient, xray XrayClient, node *NodeConfig, cfg *Config, logger *slog.Logger) *Loop {
	return &Loop{engine: e, stats: sc, core: core, xray: xray, node: node, rules: NewPlanRules(xray, logger), cfg: cfg, logger: logger, started: time.Now(),
		checker: NewEgressChecker(cfg.Egress), refillGap: xrayRefillGap}
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
	if domains := tlsDomains(l.applied); len(domains) > 0 {
		hb.TLSStatus = probeCerts(ctx, domains)
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
	ctx, cancel := context.WithTimeout(ctx, onlineBudget(l.cfg))
	defer cancel()
	raw, err := l.xray.OnlineIPs(ctx)
	if err != nil {
		l.logger.Error("heartbeat_xray_probe_failed", "probe", "online", "error", err.Error())
		return nil, false
	}
	now := time.Now().Unix()
	raw = byKey(raw)
	out := make([]OnlineKey, 0, len(raw))
	total := 0
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		k := OnlineKey{Key: key}
		for _, ip := range slices.Sorted(maps.Keys(raw[key])) {
			addr, ok := onlineAddr(ip)
			if !ok || len(k.IPs) >= maxOnlineIPsPerKey {
				continue
			}
			k.IPs = append(k.IPs, OnlineIP{IP: addr.String(), AgeSec: min(max(0, now-raw[key][ip]), maxOnlineAgeSec)})
		}
		if len(k.IPs) == 0 {
			continue
		}
		if total+len(k.IPs) > maxOnlineReport {
			l.logger.Warn("heartbeat_online_truncated", "keys", len(raw))
			break
		}
		total += len(k.IPs)
		out = append(out, k)
	}
	return out, true
}

func byKey(raw map[string]map[string]int64) map[string]map[string]int64 {
	out := make(map[string]map[string]int64, len(raw))
	for email, ips := range raw {
		uuid, _ := splitEmail(email)
		if out[uuid] == nil {
			out[uuid] = make(map[string]int64, len(ips))
		}
		for ip, since := range ips {
			if prev, ok := out[uuid][ip]; !ok || since < prev {
				out[uuid][ip] = since
			}
		}
	}
	return out
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
		for _, email := range users {
			uuid, _ := splitEmail(email)
			seen[uuid] = true
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
	if start, ok := l.xrayStarted(ctx); ok {
		l.xrayStart = start
	}
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
	var watch <-chan time.Time
	if l.cfg.XrayWatch > 0 {
		w := time.NewTicker(l.cfg.XrayWatch)
		defer w.Stop()
		watch = w.C
	}
	for {
		if err := l.RunOnce(ctx); err != nil && ctx.Err() == nil {
			l.logger.Debug("loop_tick_incomplete", "error", err.Error())
		}
	wait:
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				break wait
			case <-watch:
				l.watchXray(ctx)
			}
		}
	}
}

func (l *Loop) xrayStarted(ctx context.Context) (time.Time, bool) {
	up, err := l.xray.Uptime(ctx)
	if err != nil {
		return time.Time{}, false
	}
	return time.Now().Add(-up), true
}

func (l *Loop) watchXray(ctx context.Context) {
	start, ok := l.xrayStarted(ctx)
	if !ok {
		return
	}
	if l.xrayStart.IsZero() || start.Before(l.xrayStart) {
		l.xrayStart = start
		return
	}
	if start.Sub(l.xrayStart) <= xrayRestartSlack {
		return
	}
	if !l.lastRefill.IsZero() && time.Since(l.lastRefill) < l.refillGap {
		return
	}
	l.logger.Warn("xray_restart_detected", "uptime_sec", int64(time.Since(start)/time.Second))
	l.lastRefill = time.Now()
	if err := l.apply(ctx); err != nil {
		l.logger.Error("reconcile_failed", "error", err.Error())
		return
	}
	l.logger.Info("xray_refilled", "duration_ms", time.Since(l.lastRefill).Milliseconds())
}
