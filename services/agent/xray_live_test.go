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
	want = slices.Clone(want)
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
	if err := x.RemoveUser(ctx, xraytest.EngineTag, a); err != nil {
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
	xraytest.WaitTraffic(t, x.QueryStats, u, 100, 1000)
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
	xraytest.WaitTraffic(t, x.QueryStats, u, 150, 1300)
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
