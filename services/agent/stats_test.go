package agent

import (
	"context"
	"path/filepath"
	"testing"
)

func uplink(uuid string) string {
	return "user>>>" + uuid + ">>>traffic>>>uplink"
}

func downlink(uuid string) string {
	return "user>>>" + uuid + ">>>traffic>>>downlink"
}

func collectAndCommit(t *testing.T, sc *StatsCollector) []Delta {
	t.Helper()
	deltas, err := sc.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc.Commit(deltas)
	return deltas
}

func TestCollectDeltas(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 1000, downlink("u-1"): 500})
	sc := NewStatsCollector(fx, discardLogger())
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 1 || deltas[0].KeyUUID != "u-1" || deltas[0].BytesUp != 1000 || deltas[0].BytesDown != 500 {
		t.Fatalf("deltas = %+v", deltas)
	}
}

func TestCollectSecondTimeDelta(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 1000, downlink("u-1"): 500})
	sc := NewStatsCollector(fx, discardLogger())
	collectAndCommit(t, sc)
	fx.setStats(map[string]int64{uplink("u-1"): 1500, downlink("u-1"): 900})
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 1 || deltas[0].BytesUp != 500 || deltas[0].BytesDown != 400 {
		t.Fatalf("deltas = %+v", deltas)
	}
}

func TestDeltaAfterXrayRestart(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 5000})
	sc := NewStatsCollector(fx, discardLogger())
	collectAndCommit(t, sc)
	fx.setStats(map[string]int64{uplink("u-1"): 100})
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 1 || deltas[0].BytesUp != 100 {
		t.Fatalf("restart delta = %+v", deltas)
	}
}

func TestUncommittedDeltasAccumulate(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 1000})
	sc := NewStatsCollector(fx, discardLogger())
	if _, err := sc.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	fx.setStats(map[string]int64{uplink("u-1"): 1700})
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 1 || deltas[0].BytesUp != 1700 {
		t.Fatalf("unreported traffic lost: %+v", deltas)
	}
}

func TestCollectSkipsZero(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 0, "inbound>>>vless-in>>>traffic>>>uplink": 999})
	sc := NewStatsCollector(fx, discardLogger())
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 0 {
		t.Fatalf("deltas = %+v", deltas)
	}
}

func TestAgentRestartDoesNotDoubleCount(t *testing.T) {
	fx := newFakeXray()
	path := filepath.Join(t.TempDir(), "stats.json")
	fx.setStats(map[string]int64{uplink("u-1"): 10_000})
	first := NewStatsCollector(fx, discardLogger())
	first.path = path
	collectAndCommit(t, first)
	fx.setStats(map[string]int64{uplink("u-1"): 10_500})
	second := NewStatsCollector(fx, discardLogger())
	if err := second.Load(path); err != nil {
		t.Fatal(err)
	}
	deltas := collectAndCommit(t, second)
	if len(deltas) != 1 || deltas[0].BytesUp != 500 {
		t.Fatalf("agent restart re-sent traffic: %+v", deltas)
	}
}

func TestUnsentSurvivesXrayRestart(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 49_000})
	sc := NewStatsCollector(fx, discardLogger())
	ctx := context.Background()
	if _, err := sc.Collect(ctx); err != nil {
		t.Fatal(err)
	}
	fx.setStats(map[string]int64{uplink("u-1"): 10})
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 1 || deltas[0].BytesUp != 49_010 {
		t.Fatalf("unsent traffic lost on xray restart: %+v", deltas)
	}
}

func TestUnsentSurvivesAgentRestart(t *testing.T) {
	fx := newFakeXray()
	path := filepath.Join(t.TempDir(), "stats.json")
	fx.setStats(map[string]int64{uplink("u-1"): 700})
	first := NewStatsCollector(fx, discardLogger())
	first.path = path
	if _, err := first.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := NewStatsCollector(fx, discardLogger())
	if err := second.Load(path); err != nil {
		t.Fatal(err)
	}
	deltas := collectAndCommit(t, second)
	if len(deltas) != 1 || deltas[0].BytesUp != 700 {
		t.Fatalf("unsent traffic lost on agent restart: %+v", deltas)
	}
}

func TestRemovedUserLastTrafficReported(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("gone"): 600})
	sc := NewStatsCollector(fx, discardLogger())
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 1 || deltas[0].BytesUp != 600 {
		t.Fatalf("traffic of a just-removed user dropped: %+v", deltas)
	}
}
