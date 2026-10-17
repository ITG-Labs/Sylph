package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/yookassa"
)

func problemsByKey(t *testing.T, svc *Service) map[string]Problem {
	t.Helper()
	st, err := svc.MonitorState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Problem{}
	for _, p := range st.Problems {
		if _, dup := out[p.Key]; dup {
			t.Fatalf("duplicate problem key %s", p.Key)
		}
		out[p.Key] = p
	}
	return out
}

func exec(t *testing.T, svc *Service, q string, args ...any) {
	t.Helper()
	if _, err := poolOf(svc).Exec(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func TestMonitorStateEmptySystemHasNoProblems(t *testing.T) {
	svc := setupService(t)
	st, err := svc.MonitorState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Problems) != 0 || st.GeneratedAt.IsZero() {
		t.Fatalf("state = %+v", st)
	}
}

func TestMonitorStateNodeProblems(t *testing.T) {
	svc := setupService(t)
	broken := "xray_signal_failed: operation not permitted"
	online := mkNodeAt(t, svc, "online", true, "10 seconds", "20 seconds", nil)
	down := mkNodeAt(t, svc, "down", true, "4 minutes", "4 minutes", &broken)
	mkNodeAt(t, svc, "off", false, "4 minutes", "4 minutes", nil)
	mkNodeAt(t, svc, "new", true, "", "", nil)
	stale := mkNodeAt(t, svc, "stale", true, "10 seconds", "5 minutes", nil)
	bad := mkNodeAt(t, svc, "bad", true, "10 seconds", "10 seconds", &broken)
	exec(t, svc, `UPDATE nodes SET geo_status = '{"ru":{"error":"dial tcp: timeout"},"ok":{"size":10}}', routing_error = 'plan rules rejected' WHERE id = $1`, online.ID)
	got := problemsByKey(t, svc)
	want := map[string]string{
		nodeKey(down.ID, "offline"):   "лежит",
		nodeKey(stale.ID, "sync"):     "не синкается",
		nodeKey(bad.ID, "config"):     "xray_signal_failed",
		nodeKey(online.ID, "geo"):     "ru",
		nodeKey(online.ID, "routing"): "plan rules rejected",
	}
	for key, frag := range want {
		p, ok := got[key]
		if !ok {
			t.Errorf("missing %s", key)
			continue
		}
		if !strings.Contains(p.Text, frag) || p.Category != CategoryNode || p.DelaySec != nodeDelaySec {
			t.Errorf("%s = %+v, want text with %q, node category, delay %d", key, p, frag, nodeDelaySec)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d problems, want %d: %+v", len(got), len(want), got)
	}
	if p := got[nodeKey(down.ID, "offline")]; p.Since == nil || !strings.Contains(p.Text, "«down»") {
		t.Errorf("offline problem = %+v, want node name and since", p)
	}
}

func TestMonitorStateServices(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	for _, cn := range []string{"bot", "subs"} {
		if err := svc.ServiceHeartbeat(ctx, model.ServiceHeartbeat{CertCN: cn, Role: cn, Version: "1.0.0"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := problemsByKey(t, svc); len(got) != 0 {
		t.Fatalf("fresh services: %+v", got)
	}
	exec(t, svc, `UPDATE services SET last_seen = now() - interval '4 minutes' WHERE cert_cn = 'subs'`)
	exec(t, svc, `UPDATE services SET telegram_conflict_at = now() - interval '30 seconds' WHERE cert_cn = 'bot'`)
	got := problemsByKey(t, svc)
	if p, ok := got["service:subs:silent"]; !ok || p.Category != CategoryService || p.Since == nil {
		t.Fatalf("silent subs = %+v (%v)", p, got)
	}
	if p, ok := got["service:bot:conflict"]; !ok || !strings.Contains(p.Text, "экземпляр") {
		t.Fatalf("bot conflict = %+v (%v)", p, got)
	}
	exec(t, svc, `UPDATE services SET telegram_conflict_at = now() - interval '5 minutes' WHERE cert_cn = 'bot'`)
	if err := svc.ForgetService(ctx, "subs"); err != nil {
		t.Fatal(err)
	}
	if got := problemsByKey(t, svc); len(got) != 0 {
		t.Fatalf("after forget and old conflict: %+v", got)
	}
}

func TestMonitorStateCronAndYooKassa(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	if err := svc.MarkCronExpire(ctx); err != nil {
		t.Fatal(err)
	}
	if got := problemsByKey(t, svc); len(got) != 0 {
		t.Fatalf("fresh cron: %+v", got)
	}
	exec(t, svc, `UPDATE health_marks SET ok_at = now() - interval '6 minutes' WHERE name = 'cron_expire'`)
	exec(t, svc, `INSERT INTO health_marks (name, failing_since, failed_at, error) VALUES ('yookassa', now() - interval '6 minutes', now(), 'HTTP 503')`)
	got := problemsByKey(t, svc)
	if p, ok := got["cron:expire"]; !ok || p.Category != CategoryService {
		t.Fatalf("cron = %+v (%v)", p, got)
	}
	if p, ok := got["money:yookassa"]; !ok || p.Category != CategoryMoney || !strings.Contains(p.Text, "HTTP 503") {
		t.Fatalf("yookassa = %+v (%v)", p, got)
	}
	exec(t, svc, `UPDATE health_marks SET failing_since = now() - interval '1 minute' WHERE name = 'yookassa'`)
	if _, ok := problemsByKey(t, svc)["money:yookassa"]; ok {
		t.Fatal("a one-minute failure streak raised the alert")
	}
}

func TestMonitorStateMoney(t *testing.T) {
	svc := setupService(t)
	u, p := userAndPlan(t, svc, 501, 199)
	for _, q := range []string{
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status, source, created_at) VALUES ('mm', $1, $2, 199, 'mismatch', 'yookassa', now())`,
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status, source, created_at) VALUES ('old', $1, $2, 199, 'pending', 'yookassa', now() - interval '2 hours')`,
		`INSERT INTO payments (payment_id, user_id, plan_id, amount_rub, status, source, created_at) VALUES ('fresh', $1, $2, 199, 'pending', 'yookassa', now() - interval '5 minutes')`,
	} {
		exec(t, svc, q, u.ID, p.ID)
	}
	got := problemsByKey(t, svc)
	if p, ok := got["money:mismatch"]; !ok || p.DelaySec != 0 || !strings.Contains(p.Text, "1") {
		t.Fatalf("mismatch = %+v (%v)", p, got)
	}
	if p, ok := got["money:pending"]; !ok || !strings.Contains(p.Text, "1") {
		t.Fatalf("pending = %+v (%v)", p, got)
	}
	if _, ok := got["money:banned"]; ok {
		t.Fatal("no banned payments, but got the alert")
	}
}

func TestMonitorStateCertLevels(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()
	soon := time.Now().Add(20 * 24 * time.Hour)
	svc.SetCertExpiry(soon, time.Now().Add(10*365*24*time.Hour))
	in3 := time.Now().Add(3 * 24 * time.Hour)
	gone := time.Now().Add(-time.Hour)
	if err := svc.ServiceHeartbeat(ctx, model.ServiceHeartbeat{CertCN: "bot", Role: "bot", CertNotAfter: &in3}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ServiceHeartbeat(ctx, model.ServiceHeartbeat{CertCN: "subs", Role: "subs", CertNotAfter: &gone}); err != nil {
		t.Fatal(err)
	}
	n := mkNodeAt(t, svc, "n1", true, "10 seconds", "10 seconds", nil)
	exec(t, svc, `UPDATE nodes SET cert_not_after = now() + interval '200 days' WHERE id = $1`, n.ID)
	got := problemsByKey(t, svc)
	levels := map[string]int{"cert:core": 1, "cert:bot": 2, "cert:subs": 3}
	for key, lvl := range levels {
		p, ok := got[key]
		if !ok || p.Level != lvl || p.Category != CategoryCert {
			t.Errorf("%s = %+v, want level %d", key, p, lvl)
		}
	}
	if _, ok := got["cert:ca"]; ok {
		t.Error("a CA valid for ten years raised the alert")
	}
	if _, ok := got["cert:node:n1"]; ok {
		t.Error("a node cert valid for 200 days raised the alert")
	}
}

type failingProvider struct {
	err error
}

func (f failingProvider) CreatePayment(context.Context, yookassa.CreateRequest, string) (*yookassa.Payment, error) {
	return nil, f.err
}

func (f failingProvider) GetPayment(context.Context, string) (*yookassa.Payment, error) {
	return nil, f.err
}

func TestYooKassaOutcomesAreMarked(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"network", errors.New("dial tcp: connection refused"), "down"},
		{"server", &yookassa.APIError{Status: 503}, "down"},
		{"bad secret", &yookassa.APIError{Status: 401}, "down"},
		{"bad request", &yookassa.APIError{Status: 400}, "ok"},
		{"not found", yookassa.ErrNotFound, "ok"},
		{"caller gone", context.Canceled, "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := setupService(t)
			svc.EnablePayments(failingProvider{err: tc.err}, "https://t.me/sylph_bot")
			if _, err := svc.pay.GetPayment(context.Background(), "x"); err == nil {
				t.Fatal("provider error was swallowed")
			}
			marks, err := svc.store.HealthMarks(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			m := marks[model.HealthYooKassa]
			switch {
			case tc.want == "down" && m.FailingSince == nil:
				t.Fatalf("marks = %+v, want a failure streak", m)
			case tc.want == "ok" && (m.FailingSince != nil || m.OKAt == nil):
				t.Fatalf("marks = %+v, want ok", m)
			case tc.want == "none" && (m.FailingSince != nil || m.OKAt != nil):
				t.Fatalf("marks = %+v, want untouched", m)
			}
		})
	}
}
