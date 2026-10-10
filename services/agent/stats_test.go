package agent

import (
	"context"
	"testing"
)

func uplink(uuid string) string {
	return "user>>>" + uuid + ">>>traffic>>>uplink"
}

func downlink(uuid string) string {
	return "user>>>" + uuid + ">>>traffic>>>downlink"
}

func collectAndCommit(t *testing.T, sc *StatsCollector, active map[string]bool) []Delta {
	t.Helper()
	deltas, err := sc.Collect(context.Background(), active)
	if err != nil {
		t.Fatal(err)
	}
	sc.Commit()
	return deltas
}

func TestCollectDeltas(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 1000, downlink("u-1"): 500})
	sc := NewStatsCollector(fx, discardLogger())
	deltas := collectAndCommit(t, sc, map[string]bool{"u-1": true})
	if len(deltas) != 1 || deltas[0].KeyUUID != "u-1" || deltas[0].BytesUp != 1000 || deltas[0].BytesDown != 500 {
		t.Fatalf("deltas = %+v", deltas)
	}
}

func TestCollectSecondTimeDelta(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 1000, downlink("u-1"): 500})
	sc := NewStatsCollector(fx, discardLogger())
	active := map[string]bool{"u-1": true}
	collectAndCommit(t, sc, active)
	fx.setStats(map[string]int64{uplink("u-1"): 1500, downlink("u-1"): 900})
	deltas := collectAndCommit(t, sc, active)
	if len(deltas) != 1 || deltas[0].BytesUp != 500 || deltas[0].BytesDown != 400 {
		t.Fatalf("deltas = %+v", deltas)
	}
}

func TestDeltaAfterXrayRestart(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 5000})
	sc := NewStatsCollector(fx, discardLogger())
	active := map[string]bool{"u-1": true}
	collectAndCommit(t, sc, active)
	fx.setStats(map[string]int64{uplink("u-1"): 100})
	deltas := collectAndCommit(t, sc, active)
	if len(deltas) != 1 || deltas[0].BytesUp != 100 {
		t.Fatalf("restart delta = %+v", deltas)
	}
}

func TestUncommittedDeltasAccumulate(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 1000})
	sc := NewStatsCollector(fx, discardLogger())
	active := map[string]bool{"u-1": true}
	if _, err := sc.Collect(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	fx.setStats(map[string]int64{uplink("u-1"): 1700})
	deltas := collectAndCommit(t, sc, active)
	if len(deltas) != 1 || deltas[0].BytesUp != 1700 {
		t.Fatalf("unreported traffic lost: %+v", deltas)
	}
}

func TestCollectSkipsInactiveAndZero(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 0, uplink("ghost"): 999})
	sc := NewStatsCollector(fx, discardLogger())
	deltas := collectAndCommit(t, sc, map[string]bool{"u-1": true})
	if len(deltas) != 0 {
		t.Fatalf("deltas = %+v", deltas)
	}
}
