package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func uplink(uuid string) string {
	return "user>>>" + uuid + ">>>traffic>>>uplink"
}

func downlink(uuid string) string {
	return "user>>>" + uuid + ">>>traffic>>>downlink"
}

func collect(t *testing.T, sc *StatsCollector) *Batch {
	t.Helper()
	b, err := sc.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func collectAndCommit(t *testing.T, sc *StatsCollector) []Delta {
	t.Helper()
	b := collect(t, sc)
	if b == nil {
		return nil
	}
	sc.Commit(b.ID)
	return b.Deltas
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

func TestPendingBatchFrozenUntilCommit(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 1000})
	sc := NewStatsCollector(fx, discardLogger())
	first := collect(t, sc)
	if first == nil || first.ID == "" {
		t.Fatalf("first batch = %+v", first)
	}
	fx.setStats(map[string]int64{uplink("u-1"): 1700})
	retry := collect(t, sc)
	if retry.ID != first.ID || len(retry.Deltas) != 1 || retry.Deltas[0].BytesUp != 1000 {
		t.Fatalf("retry batch changed: %+v, first %+v", retry, first)
	}
	sc.Commit(retry.ID)
	next := collect(t, sc)
	if next == nil || next.ID == first.ID || len(next.Deltas) != 1 || next.Deltas[0].BytesUp != 700 {
		t.Fatalf("next batch = %+v, want fresh id with 700", next)
	}
}

func TestCommitOfOtherBatchIgnored(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 1000})
	sc := NewStatsCollector(fx, discardLogger())
	b := collect(t, sc)
	sc.Commit("not-" + b.ID)
	if again := collect(t, sc); again == nil || again.ID != b.ID {
		t.Fatalf("pending dropped by foreign commit: %+v", again)
	}
}

func TestCollectSkipsZero(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 0, "inbound>>>vless-in>>>traffic>>>uplink": 999})
	sc := NewStatsCollector(fx, discardLogger())
	if b := collect(t, sc); b != nil {
		t.Fatalf("batch = %+v, want none", b)
	}
}

func TestAgentRestartDoesNotDoubleCount(t *testing.T) {
	fx := newFakeXray()
	path := filepath.Join(t.TempDir(), "stats.json")
	fx.setStats(map[string]int64{uplink("u-1"): 10_000})
	first := NewStatsCollector(fx, discardLogger())
	if err := first.Load(path); err != nil {
		t.Fatal(err)
	}
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
	pending := collect(t, sc)
	fx.setStats(map[string]int64{uplink("u-1"): 10})
	retry := collect(t, sc)
	if retry.ID != pending.ID || retry.Deltas[0].BytesUp != 49_000 {
		t.Fatalf("pending changed on xray restart: %+v", retry)
	}
	sc.Commit(retry.ID)
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 1 || deltas[0].BytesUp != 10 {
		t.Fatalf("traffic after xray restart lost: %+v", deltas)
	}
}

func TestPendingSurvivesAgentRestartWithSameID(t *testing.T) {
	fx := newFakeXray()
	path := filepath.Join(t.TempDir(), "stats.json")
	fx.setStats(map[string]int64{uplink("u-1"): 700})
	first := NewStatsCollector(fx, discardLogger())
	if err := first.Load(path); err != nil {
		t.Fatal(err)
	}
	sent := collect(t, first)
	second := NewStatsCollector(fx, discardLogger())
	if err := second.Load(path); err != nil {
		t.Fatal(err)
	}
	resent := collect(t, second)
	if resent == nil || resent.ID != sent.ID || len(resent.Deltas) != 1 || resent.Deltas[0].BytesUp != 700 {
		t.Fatalf("after restart got %+v, want same batch %+v", resent, sent)
	}
}

func TestPersistFailureBlocksBatch(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{uplink("u-1"): 800})
	dir := t.TempDir()
	notDir := filepath.Join(dir, "file")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sc := NewStatsCollector(fx, discardLogger())
	sc.path = filepath.Join(notDir, "stats.json")
	b, err := sc.Collect(context.Background())
	if err == nil || b != nil {
		t.Fatalf("batch %+v, err %v; want no batch when state cannot be saved", b, err)
	}
	sc.path = filepath.Join(dir, "stats.json")
	b = collect(t, sc)
	if b == nil || len(b.Deltas) != 1 || b.Deltas[0].BytesUp != 800 {
		t.Fatalf("batch after disk recovered = %+v", b)
	}
	data, err := os.ReadFile(sc.path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk statsState
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.Pending == nil || onDisk.Pending.ID != b.ID {
		t.Fatalf("batch handed out before it was on disk: %s", data)
	}
}

func TestLoadRejectsLegacyFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	if err := os.WriteFile(path, []byte(`{"u-1":{"seen_up":1,"seen_down":0,"unsent_up":5,"unsent_down":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sc := NewStatsCollector(newFakeXray(), discardLogger())
	if err := sc.Load(path); err == nil {
		t.Fatal("legacy stats.json must be rejected, not silently dropped")
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
