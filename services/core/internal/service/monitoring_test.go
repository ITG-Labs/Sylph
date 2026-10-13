package service

import (
	"context"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkNodeAt(t *testing.T, svc *Service, cn string, enabled bool, heartbeatAgo, syncAgo string, configError *string) *model.Node {
	t.Helper()
	ctx := context.Background()
	n := &model.Node{CertCN: cn, Name: cn, Address: cn + ".example.com", Enabled: enabled}
	if err := svc.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if _, err := poolOf(svc).Exec(ctx,
		`UPDATE nodes SET last_heartbeat = now() - $2::interval, last_sync_at = now() - $3::interval, config_error = $4, active_users = 3 WHERE id = $1`,
		n.ID, nullIfEmpty(heartbeatAgo), nullIfEmpty(syncAgo), configError); err != nil {
		t.Fatal(err)
	}
	return n
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func TestNodeStatusFollowsHeartbeatAndSync(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	broken := "xray_config_rejected"
	want := map[string]string{
		"online":     NodeOnline,
		"off":        NodeDisabled,
		"silent":     NodeOffline,
		"no-sync":    NodeDegraded,
		"never-sync": NodeDegraded,
		"bad-config": NodeDegraded,
		"new":        NodeNever,
	}
	mkNodeAt(t, svc, "online", true, "10 seconds", "20 seconds", nil)
	mkNodeAt(t, svc, "off", false, "10 seconds", "20 seconds", nil)
	mkNodeAt(t, svc, "silent", true, "61 seconds", "61 seconds", nil)
	mkNodeAt(t, svc, "no-sync", true, "10 seconds", "181 seconds", nil)
	mkNodeAt(t, svc, "never-sync", true, "10 seconds", "", nil)
	mkNodeAt(t, svc, "bad-config", true, "10 seconds", "10 seconds", &broken)
	mkNodeAt(t, svc, "new", true, "", "", nil)
	nodes, err := svc.ListNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Status != want[n.CertCN] {
			t.Errorf("%s: status %q, want %q", n.CertCN, n.Status, want[n.CertCN])
		}
	}
	svc.SetNodeThresholds(2*time.Minute, 4*time.Minute)
	nodes, _ = svc.ListNodes(ctx)
	for _, n := range nodes {
		if n.CertCN == "silent" && n.Status != NodeOnline {
			t.Errorf("silent with a 2m threshold: %q", n.Status)
		}
		if n.CertCN == "no-sync" && n.Status != NodeOnline {
			t.Errorf("no-sync with a 4m threshold: %q", n.Status)
		}
	}
	one, err := svc.GetNode(ctx, nodes[0].ID)
	if err != nil || one.Status == "" {
		t.Fatalf("single node without status: %+v, %v", one, err)
	}
}

func TestOverviewCountsUsersMoneyAndProblems(t *testing.T) {
	svc, fp, _ := paymentsService(t)
	ctx := context.Background()
	mkNodeAt(t, svc, "online", true, "10 seconds", "10 seconds", nil)
	mkNodeAt(t, svc, "silent", true, "10 minutes", "10 minutes", nil)
	a, p := userAndPlan(t, svc, 701, 199)
	b, _, _ := svc.RegisterUser(ctx, 702)
	c, _, _ := svc.RegisterUser(ctx, 703)
	trial := &model.Plan{Name: "trial", PriceRub: 0, PeriodDays: 3, Trial: true, Visibility: model.VisibilityPublic}
	if err := svc.CreatePlanChecked(ctx, trial); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GrantTrial(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	paid, err := svc.CreatePayment(ctx, a.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	succeed(fp, paid.PaymentID)
	if _, err := svc.CheckPayment(ctx, paid.PaymentID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordPayment(ctx, "cash-1", c.ID, p.ID, 150); err != nil {
		t.Fatal(err)
	}
	stuck, err := svc.CreatePayment(ctx, c.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	agePayment(t, svc, stuck.PaymentID, "2 days")
	late, err := svc.CreatePayment(ctx, b.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BanUser(ctx, b.ID, ""); err != nil {
		t.Fatal(err)
	}
	succeed(fp, late.PaymentID)
	if _, err := svc.CheckPayment(ctx, late.PaymentID); err == nil {
		t.Fatal("check for a banned user answered")
	}
	ov, err := svc.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Nodes.Total != 2 || ov.Nodes.ByStatus[NodeOnline] != 1 || ov.Nodes.ByStatus[NodeOffline] != 1 || ov.Nodes.ActiveUsers != 6 || len(ov.Nodes.Nodes) != 2 {
		t.Fatalf("nodes = %+v", ov.Nodes)
	}
	if ov.Users.Total != 3 || ov.Users.Active != 2 || ov.Users.Trial != 0 || ov.Users.Banned != 1 || ov.Users.New1d != 3 || ov.Users.New30d != 3 {
		t.Fatalf("users = %+v", ov.Users)
	}
	if ov.Revenue.YooKassa.Today != 398 || ov.Revenue.YooKassa.Days30 != 398 || ov.Revenue.Manual.Today != 150 {
		t.Fatalf("revenue = %+v", ov.Revenue)
	}
	if len(ov.Revenue.ByPlan) != 1 || ov.Revenue.ByPlan[0].Amount != 398 || ov.Revenue.ByPlan[0].Count != 2 {
		t.Fatalf("revenue by plan = %+v", ov.Revenue.ByPlan)
	}
	if ov.Attention.StuckPending != 1 || ov.Attention.PaidWhileBanned != 1 || ov.Attention.NodesProblem != 1 || ov.Attention.Mismatch != 0 {
		t.Fatalf("attention = %+v", ov.Attention)
	}
	if _, err := svc.RefundPayment(ctx, paid.PaymentID, "refunded"); err != nil {
		t.Fatal(err)
	}
	ov, _ = svc.Overview(ctx)
	if ov.Revenue.YooKassa.Today != 199 || ov.Revenue.Refunded30d != 199 || ov.Users.Active != 1 {
		t.Fatalf("after refund: revenue %+v, users %+v", ov.Revenue, ov.Users)
	}
}
