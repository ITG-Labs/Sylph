package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func seedStateFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(`{"counters":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
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
	seedStateFile(t, path)
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

func TestMissingStateFileDoesNotChargeLifetimeTraffic(t *testing.T) {
	fx := newFakeXray()
	path := filepath.Join(t.TempDir(), "stats.json")
	fx.setStats(map[string]int64{uplink("u-1"): 80 << 30, downlink("u-1"): 5 << 30})
	sc := NewStatsCollector(fx, discardLogger())
	if err := sc.Load(path); err != nil {
		t.Fatal(err)
	}
	if b := collect(t, sc); b != nil {
		t.Fatalf("first collect without a state file reported %+v", b)
	}
	fx.setStats(map[string]int64{uplink("u-1"): 80<<30 + 100, downlink("u-1"): 5<<30 + 7})
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 1 || deltas[0].BytesUp != 100 || deltas[0].BytesDown != 7 {
		t.Fatalf("traffic after the baseline = %+v", deltas)
	}
	restarted := NewStatsCollector(fx, discardLogger())
	if err := restarted.Load(path); err != nil {
		t.Fatal(err)
	}
	fx.setStats(map[string]int64{uplink("u-1"): 80<<30 + 150, downlink("u-1"): 5<<30 + 7})
	deltas = collectAndCommit(t, restarted)
	if len(deltas) != 1 || deltas[0].BytesUp != 50 || deltas[0].BytesDown != 0 {
		t.Fatalf("after restart with a state file = %+v", deltas)
	}
}

func TestEmptyStateFileStillCountsNewCounters(t *testing.T) {
	fx := newFakeXray()
	path := filepath.Join(t.TempDir(), "stats.json")
	seedStateFile(t, path)
	fx.setStats(map[string]int64{uplink("u-1"): 300})
	sc := NewStatsCollector(fx, discardLogger())
	if err := sc.Load(path); err != nil {
		t.Fatal(err)
	}
	deltas := collectAndCommit(t, sc)
	if len(deltas) != 1 || deltas[0].BytesUp != 300 {
		t.Fatalf("new counter of a live state = %+v", deltas)
	}
}

func TestBatchCarriesTheTimeTrafficStarted(t *testing.T) {
	fx := newFakeXray()
	path := filepath.Join(t.TempDir(), "stats.json")
	seedStateFile(t, path)
	clock := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	newCollector := func() *StatsCollector {
		sc := NewStatsCollector(fx, discardLogger())
		sc.now = func() time.Time { return clock }
		if err := sc.Load(path); err != nil {
			t.Fatal(err)
		}
		return sc
	}
	sc := newCollector()
	fx.setStats(map[string]int64{})
	if b := collect(t, sc); b != nil {
		t.Fatalf("idle collect = %+v", b)
	}
	clock = clock.Add(time.Hour)
	fx.setStats(map[string]int64{uplink("u-1"): 100})
	first := collect(t, sc)
	if first == nil || !first.From.Equal(clock) {
		t.Fatalf("first batch = %+v, want from %v", first, clock)
	}
	started := clock
	clock = clock.Add(time.Hour)
	fx.setStats(map[string]int64{uplink("u-1"): 150})
	if again := collect(t, sc); again.ID != first.ID || !again.From.Equal(started) {
		t.Fatalf("pending batch changed: %+v", again)
	}
	clock = clock.Add(time.Hour)
	fx.setStats(map[string]int64{uplink("u-1"): 200})
	sc = newCollector()
	if again := collect(t, sc); again.ID != first.ID || !again.From.Equal(started) {
		t.Fatalf("pending batch lost its time over a restart: %+v", again)
	}
	sc.Commit(first.ID)
	clock = clock.Add(time.Hour)
	next := collect(t, sc)
	if next == nil || !next.From.Equal(started.Add(time.Hour)) || next.Deltas[0].BytesUp != 100 {
		t.Fatalf("batch after the commit = %+v, want from %v with 100 bytes", next, started.Add(time.Hour))
	}
}

func TestIdleTimeIsNotCountedIntoBatchStart(t *testing.T) {
	fx := newFakeXray()
	clock := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	sc := NewStatsCollector(fx, discardLogger())
	sc.now = func() time.Time { return clock }
	fx.setStats(map[string]int64{})
	collect(t, sc)
	clock = clock.Add(6 * time.Hour)
	fx.setStats(map[string]int64{uplink("u-1"): 5})
	b := collect(t, sc)
	if b == nil || !b.From.Equal(clock) {
		t.Fatalf("batch = %+v, want from %v", b, clock)
	}
}

func TestBatchWireFormatCarriesFromOnlyWhenKnown(t *testing.T) {
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(Batch{ID: "b", From: at, Deltas: []Delta{}})
	if err != nil || string(raw) != `{"batch_id":"b","from":"2026-11-09T12:00:00Z","deltas":[]}` {
		t.Fatalf("with from: %s, %v", raw, err)
	}
	raw, err = json.Marshal(Batch{ID: "b", Deltas: []Delta{}})
	if err != nil || string(raw) != `{"batch_id":"b","deltas":[]}` {
		t.Fatalf("without from: %s, %v", raw, err)
	}
}
