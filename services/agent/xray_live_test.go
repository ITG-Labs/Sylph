package agent

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/agent/internal/xraygrpc"
	"github.com/ITG-Labs/sylph/services/agent/internal/xraytest"
)

func liveXray(t *testing.T) *xraygrpc.Client {
	t.Helper()
	c, err := xraygrpc.Dial(xraytest.API(t), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

var liveInbounds = []Inbound{{Tag: xraytest.EngineTag}}

func liveEngine(t *testing.T, x XrayClient) *Engine {
	t.Helper()
	cfg := &Config{CachePath: filepath.Join(t.TempDir(), "cache.json")}
	e := NewEngine(cfg, nil, x, discardLogger())
	t.Cleanup(func() {
		e.setDesired(nil)
		_ = e.Reconcile(context.Background(), liveInbounds)
	})
	return e
}

func assertLiveUsers(t *testing.T, x XrayClient, want ...string) {
	t.Helper()
	got, err := x.ListUsers(context.Background(), xraytest.EngineTag)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	emails := make([]string, 0, len(want))
	for _, uuid := range want {
		emails = append(emails, userEmail(uuid, xraytest.EngineTag))
	}
	want = emails
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("xray users = %v, want %v", got, want)
	}
}

func TestLiveReconcileDrivesRealXray(t *testing.T) {
	x := liveXray(t)
	e := liveEngine(t, x)
	ctx := context.Background()
	a, b, c := xraytest.NewUUID(), xraytest.NewUUID(), xraytest.NewUUID()
	e.setDesired(users(a, b))
	if err := e.Reconcile(ctx, liveInbounds); err != nil {
		t.Fatal(err)
	}
	assertLiveUsers(t, x, a, b)
	e.setDesired(users(b, c))
	if err := e.Reconcile(ctx, liveInbounds); err != nil {
		t.Fatal(err)
	}
	assertLiveUsers(t, x, b, c)
	e.setDesired(users())
	if err := e.Reconcile(ctx, liveInbounds); err != nil {
		t.Fatal(err)
	}
	assertLiveUsers(t, x)
}

func TestLiveReconcileRestoresUsersLostByXray(t *testing.T) {
	x := liveXray(t)
	e := liveEngine(t, x)
	ctx := context.Background()
	a, b := xraytest.NewUUID(), xraytest.NewUUID()
	e.setDesired(users(a, b))
	if err := e.Reconcile(ctx, liveInbounds); err != nil {
		t.Fatal(err)
	}
	if err := x.RemoveUser(ctx, xraytest.EngineTag, userEmail(a, xraytest.EngineTag)); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(ctx, liveInbounds); err != nil {
		t.Fatal(err)
	}
	assertLiveUsers(t, x, a, b)
	if err := xraytest.Exchange(xraytest.EngineVLESS, a, []byte("back"), 4); err != nil {
		t.Fatalf("restored user cannot connect: %v", err)
	}
}

func TestLiveRevokedUserIsCutOff(t *testing.T) {
	x := liveXray(t)
	e := liveEngine(t, x)
	ctx := context.Background()
	u := xraytest.NewUUID()
	e.setDesired(users(u))
	if err := e.Reconcile(ctx, liveInbounds); err != nil {
		t.Fatal(err)
	}
	if err := xraytest.Exchange(xraytest.EngineVLESS, u, []byte("ping"), 4); err != nil {
		t.Fatalf("active user cannot connect: %v", err)
	}
	e.setDesired(users())
	if err := e.Reconcile(ctx, liveInbounds); err != nil {
		t.Fatal(err)
	}
	if err := xraytest.Exchange(xraytest.EngineVLESS, u, []byte("ping"), 4); err == nil {
		t.Fatal("revoked user still gets through")
	}
}

func deltaFor(b *Batch, uuid string) (Delta, bool) {
	if b == nil {
		return Delta{}, false
	}
	for _, d := range b.Deltas {
		if d.KeyUUID == uuid {
			return d, true
		}
	}
	return Delta{}, false
}

func TestLiveStatsCollectorCountsRealTraffic(t *testing.T) {
	x := liveXray(t)
	e := liveEngine(t, x)
	ctx := context.Background()
	u := xraytest.NewUUID()
	e.setDesired(users(u))
	if err := e.Reconcile(ctx, liveInbounds); err != nil {
		t.Fatal(err)
	}
	sc := NewStatsCollector(x, discardLogger())

	if err := xraytest.Exchange(xraytest.EngineVLESS, u, []byte(strings.Repeat("a", 100)), 1000); err != nil {
		t.Fatal(err)
	}
	xraytest.WaitTraffic(t, x.QueryStats, userEmail(u, xraytest.EngineTag), 100, 1000)
	batch, err := sc.Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := deltaFor(batch, u); !ok || d.BytesUp != 100 || d.BytesDown != 1000 {
		t.Fatalf("first delta = %+v (found %v), want 100/1000", d, ok)
	}
	sc.Commit(batch.ID)

	if err := xraytest.Exchange(xraytest.EngineVLESS, u, []byte(strings.Repeat("b", 50)), 300); err != nil {
		t.Fatal(err)
	}
	xraytest.WaitTraffic(t, x.QueryStats, userEmail(u, xraytest.EngineTag), 150, 1300)
	batch, err = sc.Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := deltaFor(batch, u); !ok || d.BytesUp != 50 || d.BytesDown != 300 {
		t.Fatalf("second delta = %+v (found %v), want only new 50/300", d, ok)
	}
	sc.Commit(batch.ID)

	batch, err = sc.Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := deltaFor(batch, u); ok {
		t.Fatalf("idle user reported again: %+v", d)
	}
}

func TestLiveKeysLiveOnTheirInboundsAndCountPerInbound(t *testing.T) {
	x := liveXray(t)
	both := []Inbound{{Tag: xraytest.EngineTag}, {Tag: xraytest.ClientTag}}
	e := NewEngine(&Config{CachePath: filepath.Join(t.TempDir(), "cache.json")}, nil, x, discardLogger())
	t.Cleanup(func() {
		e.setDesired(nil)
		_ = e.Reconcile(context.Background(), both)
	})
	ctx := context.Background()
	only, every := xraytest.NewUUID(), xraytest.NewUUID()
	e.inbounds = both
	e.setDesired([]SyncUser{{UUID: only, Inbounds: []string{xraytest.EngineTag}}, {UUID: every, Inbounds: []string{xraytest.EngineTag, xraytest.ClientTag}}})
	if err := e.Reconcile(ctx, both); err != nil {
		t.Fatal(err)
	}
	if err := xraytest.Exchange(xraytest.ClientVLESS, only, []byte("ping"), 4); err == nil {
		t.Fatal("a key reached an inbound it was not given")
	}
	sc := NewStatsCollector(x, discardLogger())
	if err := xraytest.Exchange(xraytest.EngineVLESS, every, []byte(strings.Repeat("a", 100)), 1000); err != nil {
		t.Fatal(err)
	}
	if err := xraytest.Exchange(xraytest.ClientVLESS, every, []byte(strings.Repeat("b", 30)), 300); err != nil {
		t.Fatal(err)
	}
	xraytest.WaitTraffic(t, x.QueryStats, userEmail(every, xraytest.EngineTag), 100, 1000)
	xraytest.WaitTraffic(t, x.QueryStats, userEmail(every, xraytest.ClientTag), 30, 300)
	batch, err := sc.Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Delta{}
	for _, d := range batch.Deltas {
		if d.KeyUUID == every {
			got[d.Inbound] = d
		}
	}
	if got[xraytest.EngineTag].BytesUp != 100 || got[xraytest.EngineTag].BytesDown != 1000 ||
		got[xraytest.ClientTag].BytesUp != 30 || got[xraytest.ClientTag].BytesDown != 300 || len(got) != 2 {
		t.Fatalf("per-inbound deltas = %+v", got)
	}
}
