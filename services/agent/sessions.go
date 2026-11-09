package agent

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	sessionKillTimeout = 5 * time.Second
	procRoute          = "/proc/net/route"
	procRoute6         = "/proc/net/ipv6_route"
)

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
	ctx, cancel := context.WithTimeout(ctx, onlineBudget(e.cfg))
	defer cancel()
	online, err := e.xray.OnlineIPs(ctx)
	if err != nil {
		e.logger.Error("sessions_online_failed", "error", err.Error(), "users", len(online))
	}
	drop.online = online
}

func (e *Engine) killSessions(ctx context.Context, inbounds []Inbound, drop *sessionDrop) {
	if len(drop.ips) == 0 {
		return
	}
	e.skipGateways(drop.ips)
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

func (e *Engine) skipGateways(ips map[netip.Addr]bool) {
	gateways, err := e.gateways()
	if err != nil {
		e.logger.Warn("sessions_gateways_failed", "error", err.Error())
		return
	}
	skipped := 0
	for ip := range ips {
		if gateways[ip] {
			delete(ips, ip)
			skipped++
		}
	}
	if skipped > 0 {
		e.logger.Warn("sessions_gateway_skipped", "ips", skipped, "reason", "the gateway address stands for every client behind the port proxy")
	}
}

func nodeGateways() (map[netip.Addr]bool, error) {
	route, err := os.ReadFile(procRoute)
	if err != nil {
		return nil, err
	}
	route6, err := os.ReadFile(procRoute6)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return routeGateways(string(route), string(route6)), nil
}

func routeGateways(route, route6 string) map[netip.Addr]bool {
	out := map[netip.Addr]bool{}
	for line := range strings.Lines(route) {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		v, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil || v == 0 {
			continue
		}
		var ip [4]byte
		binary.NativeEndian.PutUint32(ip[:], uint32(v))
		out[netip.AddrFrom4(ip)] = true
	}
	for line := range strings.Lines(route6) {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		raw, err := hex.DecodeString(fields[4])
		if err != nil || len(raw) != 16 {
			continue
		}
		if addr := netip.AddrFrom16([16]byte(raw)).Unmap(); !addr.IsUnspecified() {
			out[addr] = true
		}
	}
	return out
}
