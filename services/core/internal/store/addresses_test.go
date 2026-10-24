package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkDedicatedPlan(t *testing.T, s *Store, groupID *int64) *model.Plan {
	t.Helper()
	p := &model.Plan{Name: model.NewToken(), PriceRub: 500, PeriodDays: 30, LocationGroupID: groupID, DedicatedIP: true, Visibility: model.VisibilityPrivate}
	if err := s.CreatePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func report(t *testing.T, s *Store, nodeID int64, reports ...model.AddressReport) {
	t.Helper()
	if err := s.ReportNodeAddresses(context.Background(), nodeID, reports); err != nil {
		t.Fatal(err)
	}
}

func addressByIP(t *testing.T, s *Store, nodeID int64, ip string) NodeAddress {
	t.Helper()
	list, err := s.ListAddresses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.NodeID == nodeID && a.IP == ip {
			return a
		}
	}
	t.Fatalf("address %s of node %d not listed: %+v", ip, nodeID, list)
	return NodeAddress{}
}

func TestReportNodeAddresses(t *testing.T) {
	s := setupStore(t)
	n, other := mkNode(t, s, "n1"), mkNode(t, s, "n2")

	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true}, model.AddressReport{IP: "203.0.113.8", Error: "egress_network_missing"})
	a, b := addressByIP(t, s, n.ID, "203.0.113.7"), addressByIP(t, s, n.ID, "203.0.113.8")
	if a.Status != AddressOK || a.Error != nil || a.NodeName != "n1" || a.Assignment != nil {
		t.Fatalf("ok address = %+v", a)
	}
	if b.Status != AddressError || b.Error == nil || *b.Error != "egress_network_missing" {
		t.Fatalf("error address = %+v", b)
	}

	v := nodeVersion(t, s, n.CertCN)
	vo := nodeVersion(t, s, other.CertCN)
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true}, model.AddressReport{IP: "203.0.113.8", Error: "other text"})
	if nodeVersion(t, s, n.CertCN) != v {
		t.Fatal("heartbeat without status change bumped the node")
	}
	if again := addressByIP(t, s, n.ID, "203.0.113.7"); !again.LastSeenAt.After(a.LastSeenAt) {
		t.Fatalf("last_seen_at not moved: %v -> %v", a.LastSeenAt, again.LastSeenAt)
	}

	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.8", OK: true})
	if got := addressByIP(t, s, n.ID, "203.0.113.7"); got.Status != AddressMissing || got.Error != nil {
		t.Fatalf("unreported address = %+v, want missing", got)
	}
	if got := addressByIP(t, s, n.ID, "203.0.113.8"); got.Status != AddressOK || got.Error != nil {
		t.Fatalf("recovered address = %+v", got)
	}
	if nodeVersion(t, s, n.CertCN) == v {
		t.Fatal("status change did not bump the node")
	}
	if nodeVersion(t, s, other.CertCN) != vo {
		t.Fatal("another node bumped")
	}

	report(t, s, n.ID)
	for _, ip := range []string{"203.0.113.7", "203.0.113.8"} {
		if got := addressByIP(t, s, n.ID, ip); got.Status != AddressMissing {
			t.Fatalf("%s after empty report = %s", ip, got.Status)
		}
	}
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	if got := addressByIP(t, s, n.ID, "203.0.113.7"); got.Status != AddressOK || got.ID != a.ID {
		t.Fatalf("returned address = %+v, want the same row ok", got)
	}
}

func TestAssignAddress(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n, m := mkNode(t, s, "n1"), mkNode(t, s, "n2")
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true}, model.AddressReport{IP: "203.0.113.8", OK: true})
	report(t, s, m.ID, model.AddressReport{IP: "198.51.100.1", OK: true})
	a := addressByIP(t, s, n.ID, "203.0.113.7")
	b := addressByIP(t, s, n.ID, "203.0.113.8")
	c := addressByIP(t, s, m.ID, "198.51.100.1")
	u, w := mkTgUser(t, s, 101), mkUser(t, s)

	v := nodeVersion(t, s, n.CertCN)
	if err := s.AssignAddress(ctx, a.ID, u.ID, "клиент"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAddress(ctx, a.ID)
	if err != nil || got.Assignment == nil || got.Assignment.UserID != u.ID || got.Assignment.Note != "клиент" ||
		got.Assignment.Source != "admin" || got.Assignment.TelegramID == nil || *got.Assignment.TelegramID != 101 {
		t.Fatalf("assigned = %+v %v", got, err)
	}
	if nodeVersion(t, s, n.CertCN) == v {
		t.Fatal("assignment did not bump the node")
	}

	v = nodeVersion(t, s, n.CertCN)
	if err := s.AssignAddress(ctx, a.ID, u.ID, "новая заметка"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAddress(ctx, a.ID); got.Assignment.Note != "новая заметка" {
		t.Fatalf("note = %q", got.Assignment.Note)
	}
	if nodeVersion(t, s, n.CertCN) != v {
		t.Fatal("note change bumped the node")
	}

	if err := s.AssignAddress(ctx, a.ID, w.ID, ""); !errors.Is(err, ErrAddressTaken) {
		t.Fatalf("taken address: %v", err)
	}
	if err := s.AssignAddress(ctx, b.ID, u.ID, ""); !errors.Is(err, ErrUserHasNodeAddress) {
		t.Fatalf("second address on the node: %v", err)
	}
	if err := s.AssignAddress(ctx, c.ID, u.ID, ""); err != nil {
		t.Fatalf("address on another node: %v", err)
	}
	if err := s.AssignAddress(ctx, 99999, u.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown address: %v", err)
	}
	if err := s.AssignAddress(ctx, b.ID, 99999, ""); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("unknown user: %v", err)
	}

	if err := s.UnassignAddress(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnassignAddress(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unassign twice: %v", err)
	}
	if err := s.AssignAddress(ctx, a.ID, w.ID, ""); err != nil {
		t.Fatalf("freed address: %v", err)
	}
}

func TestDeleteAddressOnlyWhenMissing(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := mkNode(t, s, "n1")
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	a := addressByIP(t, s, n.ID, "203.0.113.7")
	u := mkUser(t, s)
	if err := s.AssignAddress(ctx, a.ID, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAddress(ctx, a.ID); !errors.Is(err, ErrAddressPresent) {
		t.Fatalf("delete reported address: %v", err)
	}
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", Error: "x"})
	if err := s.DeleteAddress(ctx, a.ID); !errors.Is(err, ErrAddressPresent) {
		t.Fatalf("delete address with error: %v", err)
	}
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	if _, err := s.pool.Exec(ctx, `UPDATE node_addresses SET last_seen_at = now() - interval '11 minutes'`); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAddress(ctx, a.ID); !got.Stale || got.Status != AddressOK {
		t.Fatalf("silent node's address = %+v, want stale", got)
	}
	if err := s.DeleteAddress(ctx, a.ID); err != nil {
		t.Fatalf("delete an address of a silent node: %v", err)
	}
	if _, err := s.GetAddress(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted address: %v", err)
	}
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	a = addressByIP(t, s, n.ID, "203.0.113.7")
	if err := s.AssignAddress(ctx, a.ID, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	report(t, s, n.ID)
	if err := s.DeleteAddress(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAddress(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted address: %v", err)
	}
	if list, _ := s.UserAddresses(ctx, u.ID); len(list) != 0 {
		t.Fatalf("assignment survived: %+v", list)
	}
	if err := s.DeleteAddress(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func egressOf(t *testing.T, s *Store, nodeID int64) map[string]string {
	t.Helper()
	keys, err := s.ListNodeKeys(context.Background(), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, k := range keys {
		out[k.UUID] = k.EgressIP
	}
	return out
}

func TestNodeKeysCarryEgressIP(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	n, m := mkNode(t, s, "n1"), mkNode(t, s, "n2")
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	a := addressByIP(t, s, n.ID, "203.0.113.7")
	ded := mkDedicatedPlan(t, s, nil)
	plain := mkPlan(t, s)

	u, k1 := userWithKey(t, s)
	k2 := model.NewUUID()
	if err := s.AddUserKey(ctx, u.ID, k2); err != nil {
		t.Fatal(err)
	}
	period := pay(t, s, u.ID, ded.ID, now)
	other, ko := userWithKey(t, s)
	pay(t, s, other.ID, ded.ID, now)

	if got := egressOf(t, s, n.ID); got[k1] != "" || got[ko] != "" {
		t.Fatalf("egress before assignment: %v", got)
	}
	if err := s.AssignAddress(ctx, a.ID, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	got := egressOf(t, s, n.ID)
	if got[k1] != "203.0.113.7" || got[k2] != "203.0.113.7" || got[ko] != "" {
		t.Fatalf("egress on the address node: %v", got)
	}
	if got := egressOf(t, s, m.ID); got[k1] != "" || len(got) != 3 {
		t.Fatalf("egress on another node: %v", got)
	}

	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", Error: "x"})
	if got := egressOf(t, s, n.ID); got[k1] != "" {
		t.Fatalf("egress with address error: %v", got)
	}
	report(t, s, n.ID)
	if got := egressOf(t, s, n.ID); got[k1] != "" {
		t.Fatalf("egress with missing address: %v", got)
	}
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})

	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET plan_id = $2 WHERE id = $1`, period.PeriodID, plain.ID); err != nil {
		t.Fatal(err)
	}
	if got := egressOf(t, s, n.ID); got[k1] != "" || len(got) != 3 {
		t.Fatalf("egress on a plan without dedicated_ip: %v", got)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET plan_id = $2 WHERE id = $1`, period.PeriodID, ded.ID); err != nil {
		t.Fatal(err)
	}
	if got := egressOf(t, s, n.ID); got[k1] != "203.0.113.7" {
		t.Fatalf("egress back on the dedicated plan: %v", got)
	}
}

func TestUserAddressReasons(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	g, h := mkGroup(t, s, "de"), mkGroup(t, s, "nl")
	n := mkNode(t, s, "n1")
	n.GroupID = &g.ID
	if err := s.UpdateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	a := addressByIP(t, s, n.ID, "203.0.113.7")
	u := mkUser(t, s)
	if err := s.AssignAddress(ctx, a.ID, u.ID, "к"); err != nil {
		t.Fatal(err)
	}
	reason := func(want string) {
		t.Helper()
		list, err := s.UserAddresses(ctx, u.ID)
		if err != nil || len(list) != 1 {
			t.Fatalf("user addresses = %+v %v", list, err)
		}
		if list[0].Reason != want || list[0].Effective != (want == "") || list[0].IP != "203.0.113.7" || list[0].NodeName != "n1" || list[0].Note != "к" {
			t.Fatalf("address = %+v, want reason %q", list[0], want)
		}
	}
	reason(AddressReasonNoPeriod)

	wrongGroup := mkDedicatedPlan(t, s, &h.ID)
	first := pay(t, s, u.ID, wrongGroup.ID, now)
	reason(AddressReasonLocation)

	plain := &model.Plan{Name: model.NewToken(), PriceRub: 100, PeriodDays: 30, LocationGroupID: &g.ID}
	if err := s.CreatePlan(ctx, plain); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET plan_id = $2 WHERE id = $1`, first.PeriodID, plain.ID); err != nil {
		t.Fatal(err)
	}
	reason(AddressReasonPlan)
	card, err := s.UserCard(ctx, u.ID)
	if err != nil || card.DedicatedPlan || len(card.Addresses) != 1 {
		t.Fatalf("card on a plain plan: %+v %v", card, err)
	}

	right := mkDedicatedPlan(t, s, &g.ID)
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET plan_id = $2 WHERE id = $1`, first.PeriodID, right.ID); err != nil {
		t.Fatal(err)
	}
	reason("")
	if card, err := s.UserCard(ctx, u.ID); err != nil || !card.DedicatedPlan {
		t.Fatalf("card on the dedicated plan: %+v %v", card, err)
	}

	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", Error: "x"})
	reason(AddressReasonError)
	report(t, s, n.ID)
	reason(AddressReasonMissing)
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	if _, err := s.pool.Exec(ctx, `UPDATE node_addresses SET last_seen_at = now() - interval '11 minutes'`); err != nil {
		t.Fatal(err)
	}
	reason(AddressReasonStale)
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	reason("")

	limited := &model.Plan{Name: model.NewToken(), PriceRub: 100, PeriodDays: 30, LocationGroupID: &g.ID, DedicatedIP: true, TrafficLimitBytes: new(int64(1000))}
	if err := s.CreatePlan(ctx, limited); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET plan_id = $2, traffic_used_bytes = 1000 WHERE id = $1`, first.PeriodID, limited.ID); err != nil {
		t.Fatal(err)
	}
	reason(AddressReasonTraffic)
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET plan_id = $2, traffic_used_bytes = 0 WHERE id = $1`, first.PeriodID, right.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.UserAddresses(ctx, u.ID); list[0].PlanID == nil || *list[0].PlanID != right.ID {
		t.Fatalf("plan of the address = %v, want %d", list[0].PlanID, right.ID)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE users SET status = 'banned' WHERE id = $1`, u.ID); err != nil {
		t.Fatal(err)
	}
	reason(AddressReasonBanned)
}

func TestReportNodeAddressesCapsAndForgets(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	n := mkNode(t, s, "n1")
	reports := make([]model.AddressReport, MaxNodeAddresses)
	for i := range reports {
		reports[i] = model.AddressReport{IP: fmt.Sprintf("10.0.%d.%d", i/200, i%200+1), OK: true}
	}
	report(t, s, n.ID, reports...)
	report(t, s, n.ID, append(reports, model.AddressReport{IP: "203.0.113.200", OK: true})...)
	var rows int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM node_addresses WHERE node_id = $1`, n.ID).Scan(&rows); err != nil || rows != MaxNodeAddresses {
		t.Fatalf("rows = %d %v, want the cap %d", rows, err, MaxNodeAddresses)
	}

	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	u := mkUser(t, s)
	kept := addressByIP(t, s, n.ID, "10.0.0.1")
	if err := s.AssignAddress(ctx, kept.ID, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE node_addresses SET last_seen_at = now() - interval '31 days' WHERE status = 'missing'`); err != nil {
		t.Fatal(err)
	}
	report(t, s, n.ID, model.AddressReport{IP: "203.0.113.7", OK: true})
	list, err := s.ListAddresses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("after forgetting = %+v, want the reported and the assigned one", list)
	}
}

func TestAddressWritesDoNotDeadlockWithHeartbeats(t *testing.T) {
	s := setupStore(t)
	warmPool(t, s)
	ctx := context.Background()
	n := mkNode(t, s, "n1")
	ips := []model.AddressReport{{IP: "203.0.113.7", OK: true}, {IP: "203.0.113.8", OK: true}, {IP: "203.0.113.9", OK: true}}
	report(t, s, n.ID, ips...)
	a, b := addressByIP(t, s, n.ID, "203.0.113.8"), addressByIP(t, s, n.ID, "203.0.113.9")
	u, w := mkUser(t, s), mkUser(t, s)
	var wg sync.WaitGroup
	errs := make(chan error, 400)
	for i := range 40 {
		wg.Go(func() {
			flip := []model.AddressReport{ips[0], {IP: "203.0.113.8", OK: i%2 == 0, Error: "x"}, {IP: "203.0.113.9", OK: i%3 == 0, Error: "y"}}
			errs <- s.ReportNodeAddresses(ctx, n.ID, flip)
		})
		wg.Go(func() {
			if err := s.AssignAddress(ctx, a.ID, u.ID, ""); err != nil && !errors.Is(err, ErrAddressTaken) {
				errs <- err
			}
			if err := s.UnassignAddress(ctx, a.ID); err != nil && !errors.Is(err, ErrNotFound) {
				errs <- err
			}
		})
		wg.Go(func() {
			if err := s.AssignAddress(ctx, b.ID, w.ID, ""); err != nil && !errors.Is(err, ErrAddressTaken) {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent address writes: %v", err)
		}
	}
}
