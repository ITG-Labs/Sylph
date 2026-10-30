package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/inbound"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

type subFixture struct {
	svc   *Service
	user  *model.User
	key   string
	plan  *model.Plan
	group *model.LocationGroup
}

func newSubFixture(t *testing.T, deviceLimit *int, trafficLimit *int64) *subFixture {
	t.Helper()
	svc := setupService(t)
	ctx := context.Background()
	g, err := svc.store.CreateLocationGroup(ctx, "premium")
	if err != nil {
		t.Fatal(err)
	}
	p := &model.Plan{Name: "month", PriceRub: 199, PeriodDays: 30, LocationGroupID: &g.ID, DeviceLimit: deviceLimit, HWIDRequired: deviceLimit != nil, TrafficLimitBytes: trafficLimit}
	if err := svc.store.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	u, key, err := svc.RegisterUser(ctx, 900)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.ApplyPayment(ctx, u.ID, p.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return &subFixture{svc: svc, user: u, key: key, plan: p, group: g}
}

func (f *subFixture) spend(t *testing.T, bytes int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.svc.store.GetNodeByCN(ctx, "meter"); err != nil {
		if err := f.svc.store.UpsertNode(ctx, &model.Node{CertCN: "meter", Name: "meter"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.RecordNodeDeltas(ctx, "meter", model.NewToken(), []model.TrafficDelta{{KeyUUID: f.key, BytesUp: bytes}}); err != nil {
		t.Fatal(err)
	}
}

func (f *subFixture) view(t *testing.T, hwid string) *SubscriptionView {
	t.Helper()
	v, err := f.svc.Subscription(context.Background(), SubscriptionRequest{SubToken: f.user.SubToken, HWID: hwid, UserAgent: "Happ/1.6.0", DeviceOS: "Android", DeviceModel: "Pixel 8"})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f *subFixture) node(t *testing.T, cn string, groupID *int64, enabled bool, tags ...string) *model.Node {
	t.Helper()
	ctx := context.Background()
	n := &model.Node{CertCN: cn, Name: cn, Address: cn + ".example.com", Enabled: enabled}
	if groupID != nil {
		n.GroupIDs = []int64{*groupID}
	}
	if err := f.svc.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	for _, tag := range tags {
		spec := inbound.Spec{Tag: tag, Port: 443, Label: "L-" + tag, Reality: &inbound.Reality{Dest: tag + ".example.org:443", ServerNames: []string{tag + "." + cn + ".example.org"}}}
		if _, err := f.svc.CreateInbound(ctx, n.ID, spec); err != nil {
			t.Fatal(err)
		}
	}
	return n
}

func TestSubscriptionUnknownToken(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	_, err := f.svc.Subscription(context.Background(), SubscriptionRequest{SubToken: "0123456789abcdef0123456789abcdef"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSubscriptionExpired(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	ctx := context.Background()
	sub, err := f.svc.store.GetActiveSubscription(ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.store.CancelSubscription(ctx, sub.ID); err != nil {
		t.Fatal(err)
	}
	f.node(t, "node-1", &f.group.ID, true, "main")
	v := f.view(t, "hw-1")
	if v.Status != StatusExpired || v.ExpiresAt != nil || len(v.Connections) != 0 || v.Connections == nil {
		t.Fatalf("view = %+v", v)
	}
	if n, _ := f.svc.store.CountDevices(ctx, f.user.ID); n != 0 {
		t.Fatalf("expired user registered %d devices", n)
	}
}

func TestSubscriptionTrafficExceededByUsage(t *testing.T) {
	limit := int64(1000)
	f := newSubFixture(t, nil, &limit)
	f.spend(t, 999)
	v := f.view(t, "")
	if v.Status != StatusOK || v.TrafficUsedBytes != 999 || v.TrafficLimitBytes == nil || *v.TrafficLimitBytes != 1000 || v.ExpiresAt == nil {
		t.Fatalf("under limit view = %+v", v)
	}
	f.spend(t, 1)
	if v := f.view(t, ""); v.Status != StatusTrafficExceeded {
		t.Fatalf("at limit status = %s, want traffic_exceeded", v.Status)
	}
}

func TestSubscriptionHWIDRequiredOnlyWithDeviceLimit(t *testing.T) {
	limit := 2
	f := newSubFixture(t, &limit, nil)
	if v := f.view(t, ""); v.Status != StatusHWIDRequired || len(v.Connections) != 0 {
		t.Fatalf("limited plan without hwid: %+v", v)
	}
	g := newSubFixture(t, nil, nil)
	if v := g.view(t, ""); v.Status != StatusOK {
		t.Fatalf("unlimited plan without hwid: %+v", v)
	}
	if n, _ := g.svc.store.CountDevices(context.Background(), g.user.ID); n != 0 {
		t.Fatalf("device stored without hwid: %d", n)
	}
	if v := g.view(t, "hw-free"); v.Status != StatusOK {
		t.Fatalf("unlimited plan with hwid: %+v", v)
	}
	if n, _ := g.svc.store.CountDevices(context.Background(), g.user.ID); n != 1 {
		t.Fatalf("hwid on unlimited plan not recorded: %d devices", n)
	}
}

func TestSubscriptionHWIDFlagWithoutDeviceLimit(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	f.plan.HWIDRequired = true
	if err := f.svc.store.UpdatePlan(context.Background(), f.plan); err != nil {
		t.Fatal(err)
	}
	if v := f.view(t, ""); v.Status != StatusHWIDRequired || len(v.Connections) != 0 {
		t.Fatalf("hwid plan without hwid: %+v", v)
	}
	for i, hw := range []string{"hw-1", "hw-2", "hw-3", "hw-4"} {
		if v := f.view(t, hw); v.Status != StatusOK {
			t.Fatalf("device %d on hwid plan without limit: %+v", i, v)
		}
	}
	if n, _ := f.svc.store.CountDevices(context.Background(), f.user.ID); n != 4 {
		t.Fatalf("devices = %d, want 4", n)
	}
}

func TestSubscriptionDeviceLimit(t *testing.T) {
	limit := 2
	f := newSubFixture(t, &limit, nil)
	ctx := context.Background()
	for _, hw := range []string{"hw-a", "hw-b"} {
		if v := f.view(t, hw); v.Status != StatusOK {
			t.Fatalf("%s: %+v", hw, v)
		}
	}
	if v := f.view(t, "hw-c"); v.Status != StatusDeviceLimit || len(v.Connections) != 0 {
		t.Fatalf("third device: %+v", v)
	}
	if v := f.view(t, "hw-a"); v.Status != StatusOK {
		t.Fatalf("known device after limit: %+v", v)
	}
	if n, _ := f.svc.store.CountDevices(ctx, f.user.ID); n != 2 {
		t.Fatalf("devices = %d, want 2", n)
	}
	if err := f.svc.ResetUserDevices(ctx, f.user.ID); err != nil {
		t.Fatal(err)
	}
	if v := f.view(t, "hw-c"); v.Status != StatusOK {
		t.Fatalf("after reset: %+v", v)
	}
}

func TestSubscriptionDeviceLimitHoldsUnderConcurrency(t *testing.T) {
	limit := 3
	f := newSubFixture(t, &limit, nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := f.svc.Subscription(context.Background(), SubscriptionRequest{SubToken: f.user.SubToken, HWID: "hw-" + string(rune('a'+i))})
			if err != nil {
				t.Error(err)
				return
			}
			if v.Status == StatusOK {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if allowed != 3 {
		t.Fatalf("allowed = %d, want exactly 3", allowed)
	}
}

func TestSubscriptionConnections(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	ctx := context.Background()
	other, err := f.svc.store.CreateLocationGroup(ctx, "economy")
	if err != nil {
		t.Fatal(err)
	}
	f.node(t, "alpha", &f.group.ID, true, "main", "spare")
	f.node(t, "beta", &f.group.ID, false, "main")
	f.node(t, "gamma", &other.ID, true, "main")
	f.node(t, "delta", nil, true, "main")
	f.node(t, "epsilon", &f.group.ID, true)
	v := f.view(t, "")
	if v.Status != StatusOK {
		t.Fatalf("status = %s", v.Status)
	}
	names := map[string]Connection{}
	for _, c := range v.Connections {
		names[c.Name] = c
	}
	if len(v.Connections) != 3 {
		t.Fatalf("connections = %+v", v.Connections)
	}
	for _, want := range []string{"alpha · L-main", "alpha · L-spare", "delta"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("missing %q in %+v", want, v.Connections)
		}
	}
	c := names["alpha · L-main"]
	if c.Address != "alpha.example.com" || c.Port != 443 || c.UUID != f.key || c.Flow != "xtls-rprx-vision" ||
		c.Transport != "tcp" || c.Security != "reality" || c.SNI != "main.alpha.example.org" || c.Fingerprint != "chrome" ||
		len(c.ShortID) != 16 || c.PublicKey == "" {
		t.Fatalf("connection = %+v", c)
	}
	list, err := f.svc.ListInbounds(ctx, mustNodeID(t, f.svc, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range list {
		if in.Tag == "main" && in.Reality.PublicKey != c.PublicKey {
			t.Fatal("subscription public key differs from the inbound's")
		}
	}
}

func TestSubscriptionWithoutNodesIsOKAndEmpty(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	v := f.view(t, "")
	if v.Status != StatusOK || v.Connections == nil || len(v.Connections) != 0 {
		t.Fatalf("view = %+v", v)
	}
}

func mustNodeID(t *testing.T, svc *Service, cn string) int64 {
	t.Helper()
	n, err := svc.store.GetNodeByCN(context.Background(), cn)
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func TestSubscriptionStatusOrder(t *testing.T) {
	devices := 1
	limit := int64(10)
	f := newSubFixture(t, &devices, &limit)
	ctx := context.Background()
	if v := f.view(t, "hw-1"); v.Status != StatusOK {
		t.Fatalf("setup: %+v", v)
	}
	sub, err := f.svc.store.GetActiveSubscription(ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.spend(t, 10)
	if v := f.view(t, ""); v.Status != StatusTrafficExceeded {
		t.Fatalf("traffic must win over hwid_required, got %s", v.Status)
	}
	if v := f.view(t, "hw-2"); v.Status != StatusTrafficExceeded {
		t.Fatalf("traffic must win over device_limit, got %s", v.Status)
	}
	if n, _ := f.svc.store.CountDevices(ctx, f.user.ID); n != 1 {
		t.Fatalf("over-traffic request registered a device: %d", n)
	}
	if err := f.svc.store.CancelSubscription(ctx, sub.ID); err != nil {
		t.Fatal(err)
	}
	if v := f.view(t, ""); v.Status != StatusExpired {
		t.Fatalf("expired must win over everything, got %s", v.Status)
	}
}

func TestSubscriptionSkipsNodeWithoutAddress(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	ctx := context.Background()
	legacy := &model.Node{CertCN: "legacy", Name: "legacy"}
	if err := f.svc.store.UpsertNode(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.store.SetNodeGroups(ctx, legacy.ID, []int64{f.group.ID}); err != nil {
		t.Fatal(err)
	}
	spec := inbound.Spec{Tag: "main", Port: 443, Reality: &inbound.Reality{Dest: "www.legacy.com:443", ServerNames: []string{"www.legacy.com"}}}
	if _, err := f.svc.CreateInbound(ctx, legacy.ID, spec); err != nil {
		t.Fatal(err)
	}
	f.node(t, "alpha", &f.group.ID, true, "main")
	v := f.view(t, "")
	if len(v.Connections) != 1 || v.Connections[0].Address != "alpha.example.com" {
		t.Fatalf("connections = %+v, want only the node with an address", v.Connections)
	}
}

func TestSubscriptionConnectionNamesAreUnique(t *testing.T) {
	f := newSubFixture(t, nil, nil)
	ctx := context.Background()
	add := func(cn, name, tag, label string) {
		t.Helper()
		n, err := f.svc.store.GetNodeByCN(ctx, cn)
		if errors.Is(err, store.ErrNotFound) {
			n = &model.Node{CertCN: cn, Name: name, Address: cn + ".example.com", GroupIDs: []int64{f.group.ID}, Enabled: true}
			if err := f.svc.CreateNode(ctx, n); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		spec := inbound.Spec{Tag: tag, Port: 443, Label: label, Reality: &inbound.Reality{Dest: "x.example.org:443", ServerNames: []string{tag + "." + cn + ".example.org"}}}
		if _, err := f.svc.CreateInbound(ctx, n.ID, spec); err != nil {
			t.Fatal(err)
		}
	}
	add("de-1", "Германия", "a", "REALITY")
	add("de-1", "Германия", "b", "REALITY")
	add("de-2", "Германия", "main", "")
	add("de-3", "Германия", "main", "")
	v := f.view(t, "")
	if len(v.Connections) != 4 {
		t.Fatalf("connections = %+v", v.Connections)
	}
	seen := map[string]bool{}
	for _, c := range v.Connections {
		if seen[c.Name] {
			t.Fatalf("duplicate name %q in %+v", c.Name, v.Connections)
		}
		seen[c.Name] = true
	}
	again := f.view(t, "")
	for i := range v.Connections {
		if again.Connections[i].Name != v.Connections[i].Name {
			t.Fatalf("names are not stable between requests: %q vs %q", v.Connections[i].Name, again.Connections[i].Name)
		}
	}
}
