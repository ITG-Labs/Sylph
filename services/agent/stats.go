package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type userCounters struct {
	SeenUp     int64 `json:"seen_up"`
	SeenDown   int64 `json:"seen_down"`
	UnsentUp   int64 `json:"unsent_up"`
	UnsentDown int64 `json:"unsent_down"`
}

type StatsCollector struct {
	xray   XrayClient
	logger *slog.Logger
	path   string
	state  map[string]*userCounters
}

func NewStatsCollector(xray XrayClient, logger *slog.Logger) *StatsCollector {
	return &StatsCollector{xray: xray, logger: logger, state: map[string]*userCounters{}}
}

func (sc *StatsCollector) Load(path string) error {
	sc.path = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read stats state: %w", err)
	}
	state := map[string]*userCounters{}
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse stats state: %w", err)
	}
	sc.state = state
	return nil
}

func (sc *StatsCollector) Collect(ctx context.Context) ([]Delta, error) {
	raw, err := sc.xray.QueryStats(ctx, "user>>>")
	if err != nil {
		return nil, err
	}
	current := map[string]*userCounters{}
	for name, value := range raw {
		parts := strings.Split(name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		c, ok := current[parts[1]]
		if !ok {
			c = &userCounters{}
			current[parts[1]] = c
		}
		switch parts[3] {
		case "uplink":
			c.SeenUp = value
		case "downlink":
			c.SeenDown = value
		}
	}
	for uuid, cur := range current {
		st, ok := sc.state[uuid]
		if !ok {
			st = &userCounters{}
			sc.state[uuid] = st
		}
		st.UnsentUp += counterDelta(cur.SeenUp, st.SeenUp)
		st.UnsentDown += counterDelta(cur.SeenDown, st.SeenDown)
		st.SeenUp = cur.SeenUp
		st.SeenDown = cur.SeenDown
	}
	var deltas []Delta
	for uuid, st := range sc.state {
		if st.UnsentUp == 0 && st.UnsentDown == 0 {
			if _, live := current[uuid]; !live {
				delete(sc.state, uuid)
			}
			continue
		}
		deltas = append(deltas, Delta{KeyUUID: uuid, BytesUp: st.UnsentUp, BytesDown: st.UnsentDown})
	}
	sc.persist()
	return deltas, nil
}

func (sc *StatsCollector) Commit(reported []Delta) {
	for _, d := range reported {
		st, ok := sc.state[d.KeyUUID]
		if !ok {
			continue
		}
		st.UnsentUp -= d.BytesUp
		st.UnsentDown -= d.BytesDown
	}
	sc.persist()
}

func (sc *StatsCollector) persist() {
	if sc.path == "" {
		return
	}
	if err := writeJSONAtomic(sc.path, sc.state); err != nil {
		sc.logger.Error("stats_state_save_failed", "error", err.Error())
	}
}

func counterDelta(cur int64, prev int64) int64 {
	if cur < prev {
		return cur
	}
	return cur - prev
}
