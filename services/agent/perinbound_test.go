package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

func TestEmailCarriesKeyAndInbound(t *testing.T) {
	if got := userEmail("k-1", "main"); got != "k-1@main" {
		t.Fatalf("email = %q", got)
	}
	for email, want := range map[string][2]string{"k-1@main": {"k-1", "main"}, "k-1": {"k-1", ""}, "k-1@a@b": {"k-1", "a@b"}} {
		uuid, tag := splitEmail(email)
		if uuid != want[0] || tag != want[1] {
			t.Errorf("split %q = %q %q, want %v", email, uuid, tag, want)
		}
	}
}

func TestReconcileAddsKeysOnlyToTheirInbounds(t *testing.T) {
	fx := newFakeXray()
	e := testEngine(t, fx, &fakeCore{})
	plain := testInbound("cdn")
	plain.Flow = ""
	inbounds := []Inbound{testInbound("main"), plain}
	e.inbounds = inbounds
	e.setDesired([]SyncUser{{UUID: "a", Inbounds: []string{"main"}}, {UUID: "b"}})
	if err := e.Reconcile(context.Background(), inbounds); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("main", "a") || fx.hasUser("cdn", "a") {
		t.Fatal("a must live on main only")
	}
	if !fx.hasUser("main", "b") || !fx.hasUser("cdn", "b") || fx.flowOf("cdn", "b") != "" || fx.flowOf("main", "b") != "xtls-rprx-vision" {
		t.Fatal("a key without inbounds from an old core goes everywhere with the inbound's flow")
	}
	if fx.userCount() != 3 {
		t.Fatalf("entries = %d", fx.userCount())
	}
}

func TestReconcileMovesLegacyEmailsToPerInbound(t *testing.T) {
	fx := newFakeXray()
	fx.set(func(f *fakeXray) { f.users["main/a"] = true })
	e := testEngine(t, fx, &fakeCore{})
	e.setDesired(users("a"))
	if err := e.Reconcile(context.Background(), mainIn); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("main", "a") || fx.hasEmail("main", "a") || fx.userCount() != 1 {
		t.Fatalf("legacy email left behind: %d entries", fx.userCount())
	}
}

func TestStatsReportTrafficPerInbound(t *testing.T) {
	fx := newFakeXray()
	fx.setStats(map[string]int64{
		"user>>>a@main>>>traffic>>>uplink":  10,
		"user>>>a@cdn>>>traffic>>>downlink": 5,
		"user>>>b>>>traffic>>>uplink":       3,
	})
	sc := NewStatsCollector(fx, discardLogger())
	batch, err := sc.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := slices.Clone(batch.Deltas)
	slices.SortFunc(got, func(x, y Delta) int {
		if x.KeyUUID != y.KeyUUID {
			if x.KeyUUID < y.KeyUUID {
				return -1
			}
			return 1
		}
		if x.Inbound < y.Inbound {
			return -1
		}
		return 1
	})
	want := []Delta{{KeyUUID: "a", Inbound: "cdn", BytesDown: 5}, {KeyUUID: "a", Inbound: "main", BytesUp: 10}, {KeyUUID: "b", BytesUp: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deltas = %+v", got)
	}
}

func TestOnlineMergesTheInboundsOfAKey(t *testing.T) {
	fx := newFakeXray()
	l, _ := testLoop(t, fx, deadCoreURL(t))
	now := time.Now().Unix()
	fx.set(func(f *fakeXray) {
		f.online = map[string]map[string]int64{
			"a@main": {"198.51.100.1": now - 10},
			"a@cdn":  {"198.51.100.1": now - 100, "198.51.100.2": now - 5},
		}
	})
	got, ok := l.online(context.Background())
	if !ok || len(got) != 1 || got[0].Key != "a" || len(got[0].IPs) != 2 {
		t.Fatalf("online = %+v", got)
	}
	if got[0].IPs[0].IP != "198.51.100.1" || got[0].IPs[0].AgeSec < 100 {
		t.Fatalf("the earliest session of an address must win: %+v", got[0].IPs)
	}
}

func TestActiveUsersCountsKeysNotEmails(t *testing.T) {
	fx := newFakeXray()
	l, _ := testLoop(t, fx, deadCoreURL(t))
	fx.set(func(f *fakeXray) {
		f.users["main/a@main"] = true
		f.users["cdn/a@cdn"] = true
		f.users["cdn/b@cdn"] = true
	})
	count, ok := l.activeUsers(context.Background(), testInbounds("main", "cdn"))
	if !ok || count != 2 {
		t.Fatalf("active = %d, %v", count, ok)
	}
}

func TestPlanUsersAndEgressUseEmails(t *testing.T) {
	e := testEngine(t, newFakeXray(), &fakeCore{})
	e.inbounds = testInbounds("main", "cdn")
	e.setDesired([]SyncUser{{UUID: "a", PlanID: 3, Inbounds: []string{"main"}, EgressIP: "203.0.113.7"}, {UUID: "b", PlanID: 3}})
	if got := e.PlanUsers(); !reflect.DeepEqual(got, map[int64][]string{3: {"a@main", "b@cdn", "b@main"}}) {
		t.Fatalf("plan users = %v", got)
	}
	d := e.Desired([]AddressStatus{{IP: "203.0.113.7", OK: true, Source: "203.0.113.7"}})
	if len(d.Egress) != 1 || !reflect.DeepEqual(d.Egress[0].Users, map[int64][]string{3: {"a@main"}}) {
		t.Fatalf("egress = %+v", d.Egress)
	}
	blocked := egressBlocked([]noderoute.Egress{{Name: "x", Users: map[int64][]string{3: {"a@main"}}}}, nil)
	e.Block(blocked)
	if !e.blocked["a"] {
		t.Fatalf("a held-out email must block its key: %v", e.blocked)
	}
}

func TestSyncAsksForPerInboundKeys(t *testing.T) {
	fc := &fakeCore{syncVersion: 2, syncUsers: users("a"), syncInbounds: mainIn}
	e := testEngine(t, newFakeXray(), fc)
	if _, err := e.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fc.perInbound {
		t.Fatal("the agent must ask for per-inbound keys")
	}
}

func TestLoadStateFromBeforePerInboundForcesFullSync(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(cachePath, []byte(`{"version":5,"users":[{"uuid":"u-1"}],"inbounds":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fc := &fakeCore{syncVersion: 5, syncUsers: []SyncUser{{UUID: "u-1", Inbounds: []string{"main"}}}, syncInbounds: mainIn}
	e := testEngine(t, newFakeXray(), fc)
	if err := e.LoadState(cachePath); err != nil {
		t.Fatal(err)
	}
	resp, err := e.SyncOnce(context.Background())
	if err != nil || resp.Users == nil {
		t.Fatalf("a cache from before per-inbound keys must not be trusted as current: %+v, %v", resp, err)
	}
	again := testEngine(t, newFakeXray(), fc)
	if err := again.LoadState(e.cfg.CachePath); err != nil {
		t.Fatal(err)
	}
	if again.version != 5 {
		t.Fatalf("a per-inbound cache keeps its version, got %d", again.version)
	}
}
