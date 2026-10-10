package agent

import (
	"context"
	"log/slog"
	"strings"
)

type counters struct {
	up   int64
	down int64
}

type StatsCollector struct {
	xray    XrayClient
	logger  *slog.Logger
	last    map[string]counters
	pending map[string]counters
}

func NewStatsCollector(xray XrayClient, logger *slog.Logger) *StatsCollector {
	return &StatsCollector{xray: xray, logger: logger, last: map[string]counters{}}
}

func (sc *StatsCollector) Collect(ctx context.Context, activeUsers map[string]bool) ([]Delta, error) {
	raw, err := sc.xray.QueryStats(ctx, "user>>>")
	if err != nil {
		return nil, err
	}
	current := map[string]counters{}
	for name, value := range raw {
		parts := strings.Split(name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		c := current[parts[1]]
		switch parts[3] {
		case "uplink":
			c.up = value
		case "downlink":
			c.down = value
		default:
			continue
		}
		current[parts[1]] = c
	}
	var deltas []Delta
	for uuid, cur := range current {
		if !activeUsers[uuid] {
			continue
		}
		prev := sc.last[uuid]
		up := counterDelta(cur.up, prev.up)
		down := counterDelta(cur.down, prev.down)
		if up == 0 && down == 0 {
			continue
		}
		deltas = append(deltas, Delta{KeyUUID: uuid, BytesUp: up, BytesDown: down})
	}
	sc.pending = current
	return deltas, nil
}

func (sc *StatsCollector) Commit() {
	if sc.pending == nil {
		return
	}
	sc.last = sc.pending
	sc.pending = nil
}

func counterDelta(cur int64, prev int64) int64 {
	if cur < prev {
		return cur
	}
	return cur - prev
}
