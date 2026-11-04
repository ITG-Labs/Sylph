package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

type killCall struct {
	ports       map[uint16]bool
	ips         map[netip.Addr]bool
	hasDeadline bool
	budget      time.Duration
	ctxErr      error
}

type fakeSockets struct {
	calls  []killCall
	err    error
	killed int
	before func()
}

func (f *fakeSockets) Kill(ctx context.Context, ports map[uint16]bool, ips map[netip.Addr]bool) (int, error) {
	if f.before != nil {
		f.before()
	}
	deadline, has := ctx.Deadline()
	f.calls = append(f.calls, killCall{ports: ports, ips: ips, hasDeadline: has, budget: time.Until(deadline), ctxErr: ctx.Err()})
	return f.killed, f.err
}

func killEngine(t *testing.T, fx *fakeXray, inbounds []Inbound, keys ...string) (*Engine, *fakeSockets) {
	t.Helper()
	e := testEngine(t, fx, &fakeCore{})
	fs := &fakeSockets{}
	e.sockets = fs
	e.inbounds = inbounds
	e.setDesired(users(keys...))
	if err := e.Reconcile(context.Background(), inbounds); err != nil {
		t.Fatal(err)
	}
	if len(fs.calls) != 0 {
		t.Fatalf("adding users must not touch sockets: %+v", fs.calls)
	}
	return e, fs
}

func addrs(list ...string) map[netip.Addr]bool {
	out := map[netip.Addr]bool{}
	for _, s := range list {
		out[netip.MustParseAddr(s)] = true
	}
	return out
}

func TestRemovedUserLosesOpenSessionsOfItsAddresses(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "gone", "stays")
	fx.online = map[string]map[string]int64{
		userEmail("gone", "main"):  {"203.0.113.7": 1, "[2001:db8::1]": 1},
		userEmail("stays", "main"): {"198.51.100.9": 1},
	}
	fx.afterRemove = func() { fx.online = nil }
	fs.before = func() {
		if fx.hasUser("main", "gone") {
			t.Error("sockets destroyed before RemoveUser: the client could reconnect in the gap")
		}
	}
	e.setDesired(users("stays"))
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("kill calls = %d, want 1", len(fs.calls))
	}
	if want := addrs("203.0.113.7", "2001:db8::1"); !reflect.DeepEqual(fs.calls[0].ips, want) {
		t.Fatalf("ips = %v, want %v: addresses must come from a snapshot taken before RemoveUser", fs.calls[0].ips, want)
	}
	if want := map[uint16]bool{443: true}; !reflect.DeepEqual(fs.calls[0].ports, want) {
		t.Fatalf("ports = %v, want %v", fs.calls[0].ports, want)
	}
}

func TestKillIsBoundedInTime(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "gone")
	fx.online = map[string]map[string]int64{userEmail("gone", "main"): {"203.0.113.7": 1}}
	e.setDesired(users())
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if len(fs.calls) != 1 || !fs.calls[0].hasDeadline || fs.calls[0].budget > sessionKillTimeout {
		t.Fatalf("kill call = %+v, want a deadline within %s", fs.calls, sessionKillTimeout)
	}
}

func TestNoRemovalsMeansNoSnapshotAndNoKill(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "a")
	fx.online = map[string]map[string]int64{userEmail("a", "main"): {"203.0.113.7": 1}}
	before := fx.onlineHits
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if fx.onlineHits != before || len(fs.calls) != 0 {
		t.Fatalf("online calls = %d, kill calls = %d, want none on a quiet tick", fx.onlineHits-before, len(fs.calls))
	}
}

func TestOfflineUserRemovalDoesNotKill(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "gone")
	fx.online = map[string]map[string]int64{}
	e.setDesired(users())
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if len(fs.calls) != 0 {
		t.Fatalf("kill calls = %+v, want none: nothing to destroy", fs.calls)
	}
}

func TestSnapshotIsSharedAcrossInbounds(t *testing.T) {
	fx := newFakeXray()
	inbounds := testInbounds("alpha", "beta")
	inbounds[1].Port = 8443
	e, fs := killEngine(t, fx, inbounds, "gone")
	fx.online = map[string]map[string]int64{
		userEmail("gone", "alpha"): {"203.0.113.7": 1},
		userEmail("gone", "beta"):  {"203.0.113.8": 1},
	}
	before := fx.onlineHits
	e.setDesired(users())
	if err := e.Reconcile(context.Background(), inbounds); err != nil {
		t.Fatal(err)
	}
	if fx.onlineHits-before != 1 {
		t.Fatalf("online calls = %d, want 1 per reconcile", fx.onlineHits-before)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("kill calls = %d, want 1 per reconcile", len(fs.calls))
	}
	if want := addrs("203.0.113.7", "203.0.113.8"); !reflect.DeepEqual(fs.calls[0].ips, want) {
		t.Fatalf("ips = %v, want %v", fs.calls[0].ips, want)
	}
	if want := map[uint16]bool{443: true, 8443: true}; !reflect.DeepEqual(fs.calls[0].ports, want) {
		t.Fatalf("ports = %v, want %v", fs.calls[0].ports, want)
	}
}

func TestFailedRemoveLeavesSessionsForTheRetry(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "stuck", "gone")
	fx.online = map[string]map[string]int64{
		userEmail("stuck", "main"): {"203.0.113.7": 1},
		userEmail("gone", "main"):  {"203.0.113.8": 1},
	}
	fx.failRemove = map[string]bool{"stuck": true}
	e.setDesired(users())
	if err := e.Reconcile(context.Background(), testInbounds("main")); err == nil {
		t.Fatal("failed RemoveUser must still be reported")
	}
	if len(fs.calls) != 1 || !reflect.DeepEqual(fs.calls[0].ips, addrs("203.0.113.8")) {
		t.Fatalf("kill calls = %+v, want only the removed user's address", fs.calls)
	}
	fx.failRemove = nil
	fs.calls = nil
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if len(fs.calls) != 1 || !reflect.DeepEqual(fs.calls[0].ips, addrs("203.0.113.7")) {
		t.Fatalf("retry kill calls = %+v, want the address of the user removed now", fs.calls)
	}
}

func TestKillFailureIsLoggedAndDoesNotStopReconcile(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "gone")
	var logs bytes.Buffer
	e.logger = slog.New(slog.NewTextHandler(&logs, nil))
	fx.online = map[string]map[string]int64{userEmail("gone", "main"): {"203.0.113.7": 1}}
	fs.err = errors.New("operation not permitted")
	e.setDesired(users("fresh"))
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatalf("reconcile = %v, a socket failure must not fail it", err)
	}
	if fx.hasUser("main", "gone") || !fx.hasUser("main", "fresh") {
		t.Fatal("removal and addition must both finish despite the socket failure")
	}
	if !strings.Contains(logs.String(), "sessions_kill_failed") || !strings.Contains(logs.String(), "operation not permitted") {
		t.Fatalf("logs = %s", logs.String())
	}
	if strings.Contains(logs.String(), "203.0.113.7") {
		t.Fatalf("client address leaked into logs: %s", logs.String())
	}
}

func TestSnapshotFailureDoesNotStopRemoval(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "gone")
	var logs bytes.Buffer
	e.logger = slog.New(slog.NewTextHandler(&logs, nil))
	fx.onlineErr = errors.New("xray busy")
	e.setDesired(users())
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("main", "gone") {
		t.Fatal("user must be removed even without the snapshot")
	}
	if len(fs.calls) != 0 {
		t.Fatalf("kill calls = %+v, want none without addresses", fs.calls)
	}
	if !strings.Contains(logs.String(), "sessions_online_failed") {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestUnusableAddressesAreNotTargets(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "gone")
	fx.online = map[string]map[string]int64{userEmail("gone", "main"): {
		"127.0.0.1": 1, "[::1]": 1, "0.0.0.0": 1, "[::]": 1, "garbage": 1, "": 1,
		"::ffff:203.0.113.7": 1, "[fe80::1%eth0]": 1,
	}}
	e.setDesired(users())
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if len(fs.calls) != 1 || !reflect.DeepEqual(fs.calls[0].ips, addrs("203.0.113.7", "fe80::1")) {
		t.Fatalf("kill calls = %+v", fs.calls)
	}
}

func TestBlockedUserLosesSessions(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "held")
	fx.online = map[string]map[string]int64{userEmail("held", "main"): {"203.0.113.7": 1}}
	e.Block(map[string]bool{userEmail("held", "main"): true})
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if len(fs.calls) != 1 || !reflect.DeepEqual(fs.calls[0].ips, addrs("203.0.113.7")) {
		t.Fatalf("kill calls = %+v", fs.calls)
	}
}

func TestRevokeTakesPortsFromKnownInbounds(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "gone")
	fx.online = map[string]map[string]int64{userEmail("gone", "main"): {"203.0.113.7": 1}}
	e.setDesired(users())
	if err := e.Revoke(context.Background(), []string{"main"}); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("main", "gone") {
		t.Fatal("revoke must remove the user")
	}
	if len(fs.calls) != 1 || !reflect.DeepEqual(fs.calls[0].ports, map[uint16]bool{443: true}) {
		t.Fatalf("kill calls = %+v: tag-only inbounds have no port, the known ones do", fs.calls)
	}
}

func TestNoKnownPortsSkipsKillWithWarning(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "gone")
	var logs bytes.Buffer
	e.logger = slog.New(slog.NewTextHandler(&logs, nil))
	e.inbounds = nil
	fx.online = map[string]map[string]int64{userEmail("gone", "main"): {"203.0.113.7": 1}}
	e.setDesired(users())
	if err := e.Revoke(context.Background(), []string{"main"}); err != nil {
		t.Fatal(err)
	}
	if len(fs.calls) != 0 {
		t.Fatalf("kill calls = %+v, an empty port filter must not reach the kernel", fs.calls)
	}
	if !strings.Contains(logs.String(), "sessions_kill_skipped") {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestCancelledTickStillKillsWhatItRemoved(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "gone")
	fx.online = map[string]map[string]int64{userEmail("gone", "main"): {"203.0.113.7": 1}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fx.afterRemove = cancel
	e.setDesired(users("fresh"))
	if err := e.Reconcile(ctx, testInbounds("main")); !errors.Is(err, context.Canceled) {
		t.Fatalf("reconcile = %v, want cancelled", err)
	}
	if fx.hasUser("main", "gone") {
		t.Fatal("user must be removed before the cancel")
	}
	if len(fs.calls) != 1 || fs.calls[0].ctxErr != nil || !fs.calls[0].hasDeadline {
		t.Fatalf("kill calls = %+v: nothing retries a finished RemoveUser, so the kill must still run with its own budget", fs.calls)
	}
}

func TestRotationKillsOldKeyAndKeepsNewOne(t *testing.T) {
	fx := newFakeXray()
	e, fs := killEngine(t, fx, testInbounds("main"), "old-key")
	fx.online = map[string]map[string]int64{userEmail("old-key", "main"): {"203.0.113.7": 1}}
	e.setDesired(users("new-key"))
	if err := e.Reconcile(context.Background(), testInbounds("main")); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("main", "old-key") || !fx.hasUser("main", "new-key") {
		t.Fatal("rotation must swap the keys")
	}
	if len(fs.calls) != 1 || !reflect.DeepEqual(fs.calls[0].ips, addrs("203.0.113.7")) {
		t.Fatalf("kill calls = %+v", fs.calls)
	}
}
