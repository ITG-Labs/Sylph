package agent

import (
	"context"
	"net/netip"
	"strings"
	"time"
)

const sessionKillTimeout = 5 * time.Second

type SocketKiller interface {
	Kill(ctx context.Context, ports map[uint16]bool, ips map[netip.Addr]bool) (int, error)
}

type sessionDrop struct {
	online map[string]map[string]int64
	taken  bool
	ips    map[netip.Addr]bool
}

func (d *sessionDrop) add(email string) {
	for ip := range d.online[email] {
		if addr, ok := onlineAddr(ip); ok && !addr.IsLoopback() && !addr.IsUnspecified() {
			d.ips[addr] = true
		}
	}
}

func onlineAddr(ip string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(ip, "["), "]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.WithZone("").Unmap(), true
}

func publicPorts(lists ...[]Inbound) map[uint16]bool {
	out := map[uint16]bool{}
	for _, list := range lists {
		for _, in := range list {
			if in.Port > 0 && in.Port <= 65535 {
				out[uint16(in.Port)] = true
			}
		}
	}
	return out
}

func (e *Engine) snapshotOnline(ctx context.Context, drop *sessionDrop) {
	if drop.taken {
		return
	}
	drop.taken = true
	online, err := e.xray.OnlineIPs(ctx)
	if err != nil {
		e.logger.Error("sessions_online_failed", "error", err.Error())
		return
	}
	drop.online = online
}

func (e *Engine) killSessions(ctx context.Context, inbounds []Inbound, drop *sessionDrop) {
	if len(drop.ips) == 0 {
		return
	}
	ports := publicPorts(inbounds, e.inbounds)
	if len(ports) == 0 {
		e.logger.Warn("sessions_kill_skipped", "ips", len(drop.ips), "reason", "public ports unknown")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sessionKillTimeout)
	defer cancel()
	killed, err := e.sockets.Kill(ctx, ports, drop.ips)
	if err != nil {
		e.logger.Error("sessions_kill_failed", "ips", len(drop.ips), "sockets", killed, "error", err.Error())
		return
	}
	e.logger.Info("sessions_killed", "ips", len(drop.ips), "sockets", killed)
}
