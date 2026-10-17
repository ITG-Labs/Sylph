package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func do(t *testing.T, c *http.Client, method, url, body string, hdr map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func TestServiceHeartbeatRecordsPulseAndCertExpiry(t *testing.T) {
	e := newAPIEnv(t)
	bot := e.client(t, "bot", mtls.RoleBot)
	conflict := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	code, body := do(t, bot, http.MethodPost, e.ts.URL+"/v1/services/heartbeat", `{"version":"1.2.3","uptime_sec":42,"telegram_conflict_at":"`+conflict+`"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("heartbeat = %d %s", code, body)
	}
	st, err := e.svc.MonitorState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Services) != 1 {
		t.Fatalf("services = %+v", st.Services)
	}
	sv := st.Services[0]
	if sv.CertCN != "bot" || sv.Role != "bot" || sv.Version != "1.2.3" || sv.UptimeSec != 42 || !sv.Alive || sv.TelegramConflictAt == nil {
		t.Fatalf("service = %+v", sv)
	}
	if sv.CertNotAfter == nil || time.Until(*sv.CertNotAfter) > 25*time.Hour || time.Until(*sv.CertNotAfter) < 23*time.Hour {
		t.Fatalf("cert expiry = %v, want the leaf's ~24h", sv.CertNotAfter)
	}
	for _, bad := range []string{`{"version":"` + strings.Repeat("x", 65) + `"}`, `{"uptime_sec":-1}`, `{"version":"a\u0007b"}`} {
		if code, body := do(t, bot, http.MethodPost, e.ts.URL+"/v1/services/heartbeat", bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d %s, want 400", bad, code, body)
		}
	}
}

func TestPanelPulseNeedsNoOperatorAndLeavesNoAudit(t *testing.T) {
	e := newAPIEnv(t)
	panel := e.client(t, "panel", mtls.RolePanel)
	if code, body := do(t, panel, http.MethodPost, e.ts.URL+"/v1/services/heartbeat", `{"version":"1.0.0"}`, nil); code != http.StatusOK {
		t.Fatalf("panel pulse = %d %s", code, body)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM admin_audit`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("panel pulse wrote %d audit rows", n)
	}
	if code, body := do(t, panel, http.MethodPost, e.ts.URL+"/v1/admin/groups", `{"name":"g"}`, map[string]string{"X-Sylph-Operator": "Иван"}); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("group create = %d %s", code, body)
	}
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM admin_audit`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("admin change audit rows = %d (%v), want 1: other panel routes must stay audited", n, err)
	}
}

func TestMonitorStateEndpoint(t *testing.T) {
	e := newAPIEnv(t)
	mon := e.client(t, "monitor", mtls.RoleMonitor)
	code, body := do(t, mon, http.MethodGet, e.ts.URL+"/v1/monitor/state", "", nil)
	if code != http.StatusOK {
		t.Fatalf("state = %d %s", code, body)
	}
	var st struct {
		GeneratedAt time.Time        `json:"generated_at"`
		Problems    []map[string]any `json:"problems"`
		Nodes       map[string]int   `json:"nodes"`
		Core        struct {
			Version string `json:"version"`
		} `json:"core"`
	}
	if err := json.Unmarshal(body, &st); err != nil || st.GeneratedAt.IsZero() || st.Problems == nil || st.Nodes == nil {
		t.Fatalf("state = %s (%v)", body, err)
	}
}

func TestNodeHeartbeatKeepsCertExpiry(t *testing.T) {
	e := newAPIEnv(t)
	if err := e.svc.UpsertNode(context.Background(), &model.Node{CertCN: "node-1", Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	node := e.client(t, "node-1", mtls.RoleNode)
	if code, body := do(t, node, http.MethodPost, e.ts.URL+"/v1/sync/heartbeat", `{"agent_version":"a","agent_uptime_sec":1}`, nil); code != http.StatusOK {
		t.Fatalf("heartbeat = %d %s", code, body)
	}
	n, err := e.svc.GetNodeByCN(context.Background(), "node-1")
	if err != nil || n.CertNotAfter == nil || time.Until(*n.CertNotAfter) < 23*time.Hour {
		t.Fatalf("node = %+v, %v", n, err)
	}
}

func TestOnlyCronExpireMarksCron(t *testing.T) {
	e := newAPIEnv(t)
	panel := e.client(t, "panel", mtls.RolePanel)
	op := map[string]string{"X-Sylph-Operator": "Иван"}
	if code, body := do(t, panel, http.MethodPost, e.ts.URL+"/v1/maintenance/expire", `{}`, op); code != http.StatusOK {
		t.Fatalf("panel expire = %d %s", code, body)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM health_marks WHERE name = 'cron_expire'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("a panel-triggered expire marked the cron as alive")
	}
	cron := e.client(t, "cron", mtls.RoleCron)
	if code, body := do(t, cron, http.MethodPost, e.ts.URL+"/v1/maintenance/expire", `{}`, nil); code != http.StatusOK {
		t.Fatalf("cron expire = %d %s", code, body)
	}
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM health_marks WHERE name = 'cron_expire' AND ok_at IS NOT NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("cron expire did not mark the cron")
	}
}

func TestForgetService(t *testing.T) {
	e := newAPIEnv(t)
	if code, _ := do(t, e.client(t, "subs", mtls.RoleSubs), http.MethodPost, e.ts.URL+"/v1/services/heartbeat", `{"version":"1"}`, nil); code != http.StatusOK {
		t.Fatal("subs pulse failed")
	}
	panel := e.client(t, "panel", mtls.RolePanel)
	op := map[string]string{"X-Sylph-Operator": "Иван"}
	if code, body := do(t, panel, http.MethodDelete, e.ts.URL+"/v1/admin/services/subs", "", op); code != http.StatusNoContent {
		t.Fatalf("forget = %d %s", code, body)
	}
	if code, _ := do(t, panel, http.MethodDelete, e.ts.URL+"/v1/admin/services/subs", "", op); code != http.StatusNotFound {
		t.Fatalf("second forget = %d, want 404", code)
	}
	code, body := do(t, panel, http.MethodGet, e.ts.URL+"/v1/admin/overview", "", op)
	if code != http.StatusOK || !strings.Contains(string(body), `"services":[]`) || !strings.Contains(string(body), `"core":{`) {
		t.Fatalf("overview = %d %s", code, body)
	}
}

func TestConflictTimeOnlyFromBotAndNotFromTheFuture(t *testing.T) {
	e := newAPIEnv(t)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for _, c := range []struct{ cn, role string }{{"bot", mtls.RoleBot}, {"subs", mtls.RoleSubs}} {
		if code, body := do(t, e.client(t, c.cn, c.role), http.MethodPost, e.ts.URL+"/v1/services/heartbeat", `{"version":"1","telegram_conflict_at":"`+future+`"}`, nil); code != http.StatusOK {
			t.Fatalf("%s pulse = %d %s", c.cn, code, body)
		}
	}
	st, err := e.svc.MonitorState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, sv := range st.Services {
		switch sv.CertCN {
		case "bot":
			if sv.TelegramConflictAt == nil || sv.TelegramConflictAt.After(time.Now().Add(time.Minute)) {
				t.Fatalf("bot conflict = %v, want clamped to now", sv.TelegramConflictAt)
			}
		case "subs":
			if sv.TelegramConflictAt != nil {
				t.Fatalf("subs conflict = %v, want ignored", sv.TelegramConflictAt)
			}
		}
	}
}
