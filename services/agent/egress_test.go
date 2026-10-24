package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

func fakeInterfaces(t *testing.T, ifaces map[string][]string) {
	t.Helper()
	prev := ifaceAddrs
	t.Cleanup(func() { ifaceAddrs = prev })
	ifaceAddrs = func(name string) ([]net.Addr, error) {
		addrs, ok := ifaces[name]
		if !ok {
			return nil, errors.New("route ip+net: no such network interface")
		}
		out := make([]net.Addr, 0, len(addrs))
		for _, a := range addrs {
			ip, n, err := net.ParseCIDR(a)
			if err != nil {
				t.Fatal(err)
			}
			n.IP = ip
			out = append(out, n)
		}
		return out, nil
	}
}

func TestParseEgress(t *testing.T) {
	got, err := ParseEgress(" 203.0.113.7=egress1, 203.0.113.8=egress2 ,")
	want := []EgressAddr{{IP: "203.0.113.7", Iface: "egress1"}, {IP: "203.0.113.8", Iface: "egress2"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("parse = %+v %v", got, err)
	}
	if got, err := ParseEgress(""); err != nil || got != nil {
		t.Fatalf("empty = %+v %v", got, err)
	}
	for _, bad := range []string{
		"203.0.113.7",
		"203.0.113.7=",
		"2001:db8::1=egress1",
		"203.0.113.7=Egress1",
		"203.0.113.7=egress-1",
		"203.0.113.7=averyverylongname",
		"203.0.113.07=egress1",
		"203.0.113.7=egress1,203.0.113.7=egress2",
		"203.0.113.7=egress1,203.0.113.8=egress1",
	} {
		if _, err := ParseEgress(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestEgressStatus(t *testing.T) {
	fakeInterfaces(t, map[string][]string{"egress1": {"fe80::1/64", "172.19.0.2/16"}, "egress3": {"fe80::3/64"}})
	addrs := []EgressAddr{{IP: "203.0.113.7", Iface: "egress1"}, {IP: "203.0.113.8", Iface: "egress2"}, {IP: "203.0.113.9", Iface: "egress3"}}
	got := egressStatus(addrs)
	if len(got) != 3 || !got[0].OK || got[0].IP != "203.0.113.7" || got[0].Source != "172.19.0.2" || got[0].Error != "" {
		t.Fatalf("present = %+v", got)
	}
	if got[1].OK || got[1].Source != "" || !strings.HasPrefix(got[1].Error, "egress_network_missing: interface egress2") {
		t.Fatalf("missing = %+v", got[1])
	}
	if got[2].OK || !strings.Contains(got[2].Error, "no IPv4 address") {
		t.Fatalf("without IPv4 = %+v", got[2])
	}
	if got := egressStatus(nil); got == nil || len(got) != 0 {
		t.Fatalf("no addresses = %#v, want an empty list", got)
	}
}

func TestBuildEgress(t *testing.T) {
	status := []AddressStatus{{IP: "203.0.113.8", OK: true, Source: "172.31.227.10"}, {IP: "203.0.113.7", OK: true, Source: "172.31.226.10"}, {IP: "203.0.113.9", Source: "172.31.228.10"}}
	got := buildEgress(status, []SyncUser{
		{UUID: "k2", PlanID: 3, EgressIP: "203.0.113.7"},
		{UUID: "k1", PlanID: 3, EgressIP: "203.0.113.7"},
		{UUID: "k3", PlanID: 5, EgressIP: "203.0.113.7"},
		{UUID: "k4", PlanID: 3, EgressIP: "203.0.113.8"},
		{UUID: "k5", PlanID: 3, EgressIP: "203.0.113.9"},
		{UUID: "k6", PlanID: 3, EgressIP: "198.51.100.1"},
		{UUID: "k7", PlanID: 3},
	})
	want := []noderoute.Egress{
		{Name: "203-0-113-7", SendThrough: "172.31.226.10", Users: map[int64][]string{3: {"k1", "k2"}, 5: {"k3"}}},
		{Name: "203-0-113-8", SendThrough: "172.31.227.10", Users: map[int64][]string{3: {"k4"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("egress =\n%+v\nwant\n%+v", got, want)
	}
	if got := buildEgress(status, []SyncUser{{UUID: "k7", PlanID: 3}}); len(got) != 0 {
		t.Fatalf("egress without bound users = %+v", got)
	}
}

func TestBuildXrayEgressWithoutProfile(t *testing.T) {
	data, err := BuildXrayRouted(XrayInput{Inbounds: testInbounds("main"), APIListen: "127.0.0.1:10085",
		Egress: []noderoute.Egress{{Name: "203-0-113-7", SendThrough: "172.31.226.10", Users: map[int64][]string{5: {"k3"}, 3: {"k2", "k1"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
		Routing   struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Outbounds) != 2 || cfg.Outbounds[0]["tag"] != "direct" || cfg.Outbounds[1]["tag"] != "_egress-203-0-113-7-direct" ||
		cfg.Outbounds[1]["sendThrough"] != "172.31.226.10" {
		t.Fatalf("outbounds = %v", cfg.Outbounds)
	}
	if len(cfg.Routing.Rules) != 1 || cfg.Routing.Rules[0]["outboundTag"] != "_egress-203-0-113-7-direct" ||
		!reflect.DeepEqual(cfg.Routing.Rules[0]["user"], []any{"k1", "k2", "k3"}) {
		t.Fatalf("rules = %v", cfg.Routing.Rules)
	}
	plain, err := BuildXray(testInbounds("main"), "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "routing") || strings.Contains(string(plain), "_egress") {
		t.Fatalf("config without egress changed: %s", plain)
	}
}

func fakeProbe(t *testing.T, seen map[string]string) *int {
	t.Helper()
	prev := egressProbe
	t.Cleanup(func() { egressProbe = prev })
	calls := 0
	egressProbe = func(ctx context.Context, source string) (string, error) {
		calls++
		ip, ok := seen[source]
		if !ok {
			return "", errors.New("dial tcp 1.1.1.1:443: i/o timeout")
		}
		return ip, nil
	}
	return &calls
}

func TestLoopAppliesEgressAndReportsAddresses(t *testing.T) {
	fakeInterfaces(t, map[string][]string{"eth0": {"172.18.0.2/16"}, "egress1": {"172.31.226.10/24"}})
	fakeProbe(t, map[string]string{"172.31.226.10": "203.0.113.7"})
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1", PlanID: 3, EgressIP: "203.0.113.7"}, {UUID: "u-2", PlanID: 3}}, syncInbounds: mainIn}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	loop.cfg.Egress = []EgressAddr{{IP: "203.0.113.7", Iface: "egress1"}, {IP: "203.0.113.8", Iface: "egress2"}}
	loop.checker = NewEgressChecker(loop.cfg.Egress)
	preapply(t, loop, mainIn)
	tick(t, loop)

	disk, err := os.ReadFile(loop.cfg.XrayConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(disk), `"sendThrough": "172.31.226.10"`) || !strings.Contains(string(disk), `"u-1"`) || strings.Contains(string(disk), `"u-2"`) ||
		strings.Contains(string(disk), "203-0-113-8") {
		t.Fatalf("xray config does not route only u-1 through its address: %s", disk)
	}
	hb, _ := lastHeartbeat(fc)
	got, _ := json.Marshal(hb["addresses"])
	if string(got) != `[{"ip":"203.0.113.7","ok":true},{"error":"egress_network_missing: interface egress2: route ip+net: no such network interface","ip":"203.0.113.8","ok":false}]` {
		t.Fatalf("heartbeat addresses = %s", got)
	}

	restored := loop.node
	again := NewNodeConfig(loop.cfg, fx, NewCaddyAPI(loop.cfg.CaddyAdmin, 2*time.Second), discardLogger())
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.state.AppliedEgress, restored.state.AppliedEgress) || len(again.state.AppliedEgress) != 1 {
		t.Fatalf("applied egress after reload = %+v, want %+v", again.state.AppliedEgress, restored.state.AppliedEgress)
	}
}

func TestHeartbeatSendsEmptyAddressesWithoutEgress(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1"}}, syncInbounds: mainIn}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	preapply(t, loop, mainIn)
	tick(t, loop)
	hb, _ := lastHeartbeat(fc)
	if got, ok := hb["addresses"].([]any); !ok || len(got) != 0 {
		t.Fatalf("addresses = %#v, want [] so the core marks removed addresses missing", hb["addresses"])
	}
}

func TestParseEgressCap(t *testing.T) {
	items := make([]string, maxEgress+1)
	for i := range items {
		items[i] = fmt.Sprintf("203.0.113.%d=eg%d", i+1, i+1)
	}
	if _, err := ParseEgress(strings.Join(items[:maxEgress], ",")); err != nil {
		t.Fatalf("the cap itself refused: %v", err)
	}
	if _, err := ParseEgress(strings.Join(items, ",")); err == nil {
		t.Fatal("more than the cap accepted")
	}
}

func TestEgressCheckerProbesFromTheSource(t *testing.T) {
	fakeInterfaces(t, map[string][]string{"egress1": {"172.31.226.10/24"}, "egress2": {"172.31.227.10/24"}, "egress3": {"172.31.228.10/24"}})
	calls := fakeProbe(t, map[string]string{"172.31.226.10": "203.0.113.7", "172.31.227.10": "198.51.100.99"})
	now := time.Now()
	c := NewEgressChecker([]EgressAddr{{IP: "203.0.113.7", Iface: "egress1"}, {IP: "203.0.113.8", Iface: "egress2"}, {IP: "203.0.113.9", Iface: "egress3"}})
	c.now = func() time.Time { return now }
	c.retry = time.Millisecond
	st := c.Status(context.Background())
	if !st[0].OK || st[0].Source != "172.31.226.10" {
		t.Fatalf("working address = %+v", st[0])
	}
	if st[1].OK || !strings.Contains(st[1].Error, "egress_probe_mismatch") || !strings.Contains(st[1].Error, "198.51.100.99") {
		t.Fatalf("address the hoster rewrites = %+v", st[1])
	}
	if st[2].OK || !strings.HasPrefix(st[2].Error, "egress_probe_failed") {
		t.Fatalf("address without egress = %+v", st[2])
	}
	if *calls != 4 {
		t.Fatalf("probes = %d, want one per address and a retry for the dead one", *calls)
	}
	c.Status(context.Background())
	if *calls != 4 {
		t.Fatalf("probes repeated within the cache: %d", *calls)
	}
	now = now.Add(egressProbeRetry)
	c.Status(context.Background())
	if *calls != 7 {
		t.Fatalf("failing addresses not re-probed after a minute, or the working one re-probed early: %d probes", *calls)
	}
	now = now.Add(egressProbeEvery)
	c.Status(context.Background())
	if *calls != 11 {
		t.Fatalf("working address not re-probed after five minutes: %d probes", *calls)
	}
	fakeInterfaces(t, map[string][]string{"egress1": {"172.31.240.10/24"}, "egress2": {"172.31.227.10/24"}, "egress3": {"172.31.228.10/24"}})
	before := *calls
	if st := c.Status(context.Background()); st[0].OK || st[0].Source != "172.31.240.10" || *calls != before+2 {
		t.Fatalf("new container address not probed at once: %+v, %d probes", st[0], *calls-before)
	}
}

func TestEgressBlocked(t *testing.T) {
	applied := []noderoute.Egress{{Name: "a", Users: map[int64][]string{3: {"u1", "u2"}}}, {Name: "b", Users: map[int64][]string{3: {"u3"}}}}
	desired := []noderoute.Egress{{Name: "a", Users: map[int64][]string{5: {"u1"}}}, {Name: "c", Users: map[int64][]string{3: {"u3"}}}, {Name: "b", Users: map[int64][]string{3: {"u4"}}}}
	got := egressBlocked(applied, desired)
	if !reflect.DeepEqual(got, map[string]bool{"u2": true, "u3": true}) {
		t.Fatalf("blocked = %v: u2 lost its address, u3 moved; u1 only changed plan, u4 is new", got)
	}
	if got := egressBlocked(nil, desired); len(got) != 0 {
		t.Fatalf("nothing applied yet, blocked = %v", got)
	}
}

func TestBlockedUsersAreKeptOutOfXray(t *testing.T) {
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 1, syncUsers: users("u1", "u2"), syncInbounds: mainIn}
	e := testEngine(t, fx, fc)
	if _, err := e.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(context.Background(), mainIn); err != nil {
		t.Fatal(err)
	}
	e.Block(map[string]bool{"u1": true})
	if err := e.Reconcile(context.Background(), mainIn); err != nil {
		t.Fatal(err)
	}
	if fx.hasUser("main", "u1") || !fx.hasUser("main", "u2") {
		t.Fatal("blocked user still served, or the other one dropped")
	}
	e.Block(nil)
	if err := e.Reconcile(context.Background(), mainIn); err != nil {
		t.Fatal(err)
	}
	if !fx.hasUser("main", "u1") {
		t.Fatal("unblocked user not let back in")
	}

	fresh := newFakeXray()
	again := testEngine(t, fresh, fc)
	if _, err := again.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	again.Block(map[string]bool{"u1": true})
	if err := again.Reconcile(context.Background(), mainIn); err != nil {
		t.Fatal(err)
	}
	if fresh.hasUser("main", "u1") || !fresh.hasUser("main", "u2") {
		t.Fatal("blocked user added to an xray that did not have it yet")
	}
}

func TestEgressChangesAreSpacedAndHeldOutMeanwhile(t *testing.T) {
	fakeInterfaces(t, map[string][]string{"egress1": {"172.31.226.10/24"}})
	fakeProbe(t, map[string]string{"172.31.226.10": "203.0.113.7"})
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1", PlanID: 3}, {UUID: "u-2", PlanID: 3}}, syncInbounds: mainIn}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	loop.cfg.Egress = []EgressAddr{{IP: "203.0.113.7", Iface: "egress1"}}
	loop.checker = NewEgressChecker(loop.cfg.Egress)
	preapply(t, loop, mainIn)
	tick(t, loop)
	if loop.node.AppliedEgress() != nil {
		t.Fatalf("egress without bound users: %+v", loop.node.AppliedEgress())
	}

	fc.mu.Lock()
	fc.syncVersion, fc.syncUsers = 3, []SyncUser{{UUID: "u-1", PlanID: 3, EgressIP: "203.0.113.7"}, {UUID: "u-2", PlanID: 3}}
	fc.mu.Unlock()
	tick(t, loop)
	if names := egressNames(loop.node.AppliedEgress()); names["u-1"] != "203-0-113-7" {
		t.Fatalf("first binding not applied at once: %+v", loop.node.AppliedEgress())
	}

	fc.mu.Lock()
	fc.syncVersion, fc.syncUsers = 4, []SyncUser{{UUID: "u-1", PlanID: 3}, {UUID: "u-2", PlanID: 3, EgressIP: "203.0.113.7"}}
	fc.mu.Unlock()
	tick(t, loop)
	if names := egressNames(loop.node.AppliedEgress()); names["u-1"] != "203-0-113-7" || names["u-2"] != "" {
		t.Fatalf("second change within the cooldown applied: %+v", loop.node.AppliedEgress())
	}
	if fx.hasUser("main", "u-1") {
		t.Fatal("u-1 still served while the running config sends it through an address that is no longer its own")
	}
	if !fx.hasUser("main", "u-2") {
		t.Fatal("u-2 cut off while its address is pending")
	}

	loop.node.egressAt = time.Now().Add(-egressCooldown)
	tick(t, loop)
	if names := egressNames(loop.node.AppliedEgress()); names["u-2"] != "203-0-113-7" || names["u-1"] != "" {
		t.Fatalf("change not applied after the cooldown: %+v", loop.node.AppliedEgress())
	}
	if !fx.hasUser("main", "u-1") {
		t.Fatal("u-1 not let back in once the config moved on")
	}
}
