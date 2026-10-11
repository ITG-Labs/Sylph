package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

type userCounters struct {
	SeenUp     int64 `json:"seen_up"`
	SeenDown   int64 `json:"seen_down"`
	UnsentUp   int64 `json:"unsent_up"`
	UnsentDown int64 `json:"unsent_down"`
}

type Batch struct {
	ID     string  `json:"batch_id"`
	Deltas []Delta `json:"deltas"`
}

type statsState struct {
	Counters map[string]*userCounters `json:"counters"`
	Pending  *Batch                   `json:"pending"`
}

type StatsCollector struct {
	xray   XrayClient
	logger *slog.Logger
	path   string
	state  statsState
}

func NewStatsCollector(xray XrayClient, logger *slog.Logger) *StatsCollector {
	return &StatsCollector{xray: xray, logger: logger, state: statsState{Counters: map[string]*userCounters{}}}
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
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var st statsState
	if err := dec.Decode(&st); err != nil {
		return fmt.Errorf("parse stats state: %w", err)
	}
	if st.Counters == nil {
		st.Counters = map[string]*userCounters{}
	}
	sc.state = st
	return nil
}

func (sc *StatsCollector) Collect(ctx context.Context) (*Batch, error) {
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
		st, ok := sc.state.Counters[uuid]
		if !ok {
			st = &userCounters{}
			sc.state.Counters[uuid] = st
		}
		st.UnsentUp += counterDelta(cur.SeenUp, st.SeenUp)
		st.UnsentDown += counterDelta(cur.SeenDown, st.SeenDown)
		st.SeenUp = cur.SeenUp
		st.SeenDown = cur.SeenDown
	}
	for uuid, st := range sc.state.Counters {
		if st.UnsentUp == 0 && st.UnsentDown == 0 {
			if _, live := current[uuid]; !live {
				delete(sc.state.Counters, uuid)
			}
		}
	}
	if sc.state.Pending == nil {
		sc.freezeBatch()
	}
	if err := sc.persist(); err != nil {
		return nil, fmt.Errorf("save stats state: %w", err)
	}
	return sc.state.Pending, nil
}

func (sc *StatsCollector) freezeBatch() {
	var deltas []Delta
	for uuid, st := range sc.state.Counters {
		if st.UnsentUp == 0 && st.UnsentDown == 0 {
			continue
		}
		deltas = append(deltas, Delta{KeyUUID: uuid, BytesUp: st.UnsentUp, BytesDown: st.UnsentDown})
		st.UnsentUp = 0
		st.UnsentDown = 0
	}
	if len(deltas) > 0 {
		sc.state.Pending = &Batch{ID: logging.NewRequestID(), Deltas: deltas}
	}
}

func (sc *StatsCollector) Commit(batchID string) {
	if sc.state.Pending == nil || sc.state.Pending.ID != batchID {
		return
	}
	sc.state.Pending = nil
	if err := sc.persist(); err != nil {
		sc.logger.Error("stats_state_save_failed", "error", err.Error())
	}
}

func (sc *StatsCollector) persist() error {
	if sc.path == "" {
		return nil
	}
	return writeJSONAtomic(sc.path, sc.state)
}

func counterDelta(cur int64, prev int64) int64 {
	if cur < prev {
		return cur
	}
	return cur - prev
}
