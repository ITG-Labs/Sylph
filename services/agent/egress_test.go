package agent

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
)

func fakeInterfaces(t *testing.T, addrs ...string) {
	t.Helper()
	prev := interfaceAddrs
	t.Cleanup(func() { interfaceAddrs = prev })
	interfaceAddrs = func() ([]net.Addr, error) {
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
	got, err := ParseEgress(" 203.0.113.7=172.31.226.10, 203.0.113.8=172.31.227.10 ,")
	want := []EgressAddr{{IP: "203.0.113.7", Source: "172.31.226.10"}, {IP: "203.0.113.8", Source: "172.31.227.10"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("parse = %+v %v", got, err)
	}
	if got, err := ParseEgress(""); err != nil || got != nil {
		t.Fatalf("empty = %+v %v", got, err)
	}
	for _, bad := range []string{
		"203.0.113.7",
		"203.0.113.7=",
		"2001:db8::1=172.31.226.10",
		"203.0.113.7=fe80::1",
		"203.0.113.07=172.31.226.10",
		"203.0.113.7=172.31.226.10,203.0.113.7=172.31.227.10",
		"203.0.113.7=172.31.226.10,203.0.113.8=172.31.226.10",
	} {
		if _, err := ParseEgress(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestEgressStatus(t *testing.T) {
	fakeInterfaces(t, "127.0.0.1/8", "172.18.0.2/16", "172.31.226.10/24")
	addrs := []EgressAddr{{IP: "203.0.113.7", Source: "172.31.226.10"}, {IP: "203.0.113.8", Source: "172.31.227.10"}}
	got := egressStatus(addrs)
	if len(got) != 2 || !got[0].OK || got[0].IP != "203.0.113.7" || got[0].Error != "" ||
		got[1].OK || !strings.HasPrefix(got[1].Error, "egress_network_missing") || !strings.Contains(got[1].Error, "172.31.227.10") {
		t.Fatalf("status = %+v", got)
	}
	if got := egressStatus(nil); got == nil || len(got) != 0 {
		t.Fatalf("no addresses = %#v, want an empty list", got)
	}
	interfaceAddrs = func() ([]net.Addr, error) { return nil, errors.New("boom") }
	if got := egressStatus(addrs); got[0].OK || !strings.HasPrefix(got[0].Error, "egress_interfaces_unreadable") {
		t.Fatalf("unreadable = %+v", got)
	}
}

func TestBuildEgress(t *testing.T) {
	addrs := []EgressAddr{{IP: "203.0.113.8", Source: "172.31.227.10"}, {IP: "203.0.113.7", Source: "172.31.226.10"}, {IP: "203.0.113.9", Source: "172.31.228.10"}}
	status := []AddressStatus{{IP: "203.0.113.8", OK: true}, {IP: "203.0.113.7", OK: true}, {IP: "203.0.113.9"}}
	got := buildEgress(addrs, status, []SyncUser{
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
	if got := buildEgress(addrs, status, []SyncUser{{UUID: "k7", PlanID: 3}}); len(got) != 0 {
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

func TestLoopAppliesEgressAndReportsAddresses(t *testing.T) {
	fakeInterfaces(t, "172.18.0.2/16", "172.31.226.10/24")
	fx := newFakeXray()
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{{UUID: "u-1", PlanID: 3, EgressIP: "203.0.113.7"}, {UUID: "u-2", PlanID: 3}}, syncInbounds: mainIn}
	loop, _ := testLoop(t, fx, httptestOf(fc, t).URL)
	loop.cfg.Egress = []EgressAddr{{IP: "203.0.113.7", Source: "172.31.226.10"}, {IP: "203.0.113.8", Source: "172.31.227.10"}}
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
	if string(got) != `[{"ip":"203.0.113.7","ok":true},{"error":"egress_network_missing: no interface of this container has address 172.31.227.10","ip":"203.0.113.8","ok":false}]` {
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
