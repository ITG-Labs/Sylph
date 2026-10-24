package compose

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func TestParsePsNDJSONAndArray(t *testing.T) {
	nd := `{"ID":"aaa","Service":"core","State":"running","Health":"healthy","ExitCode":0,"Image":"ghcr.io/itg-labs/sylph-core:0.1.0"}
{"ID":"bbb","Service":"perms","State":"exited","Health":"","ExitCode":0,"Image":"alpine:3.24.2"}
`
	arr := `[{"ID":"aaa","Service":"core","State":"running","Health":"healthy","ExitCode":0},{"ID":"bbb","Service":"perms","State":"exited","ExitCode":0}]`
	for name, in := range map[string]string{"ndjson": nd, "array": arr} {
		cs, err := parsePs([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(cs) != 2 || cs[0].Service != "core" || cs[0].Health != "healthy" || cs[1].State != "exited" {
			t.Fatalf("%s: %+v", name, cs)
		}
	}
	cs, err := parsePs([]byte("\n"))
	if err != nil || len(cs) != 0 {
		t.Fatalf("empty = %v, %v", cs, err)
	}
}

func TestApplyInspect(t *testing.T) {
	cs := []Container{{ID: "aaa"}, {ID: "bbb"}}
	applyInspect(cs, []byte("aaa111|2026-10-17T11:59:00.123456789Z|0|0\nbbb222|2026-10-17T11:00:00Z|4|1\n"))
	if cs[0].StartedAt.IsZero() || cs[1].RestartCount != 4 || cs[1].ExitCode != 1 {
		t.Fatalf("%+v", cs)
	}
}

func TestParseLs(t *testing.T) {
	ps, err := parseLs([]byte(`[{"Name":"sylph-core","Status":"running(3)","ConfigFiles":"/home/u/sylph-core/compose.yml"},{"Name":"x","Status":"exited(1)","ConfigFiles":"/srv/x/compose.yml,/srv/x/compose.override.yml"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].Dir != "/home/u/sylph-core" || ps[1].Dir != "/srv/x" {
		t.Fatalf("%+v", ps)
	}
}

func TestParseConfig(t *testing.T) {
	svcs, err := parseConfig([]byte(`{"services":{"perms":{},"core":{"restart":"unless-stopped"},"bootstrap":{"restart":"no"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"bootstrap": true, "core": false, "perms": true}
	for _, s := range svcs {
		if want[s.Name] != s.OneShot {
			t.Fatalf("%s oneShot = %v", s.Name, s.OneShot)
		}
	}
}

func TestEvaluate(t *testing.T) {
	services := []Service{{Name: "perms", OneShot: true}, {Name: "core"}, {Name: "caddy"}, {Name: "cron"}}
	cs := []Container{
		{Service: "perms", State: "exited", ExitCode: 0},
		{Service: "core", State: "running", Health: "starting"},
		{Service: "caddy", State: "running", StartedAt: now.Add(-5 * time.Second)},
	}
	v := Evaluate(services, cs, now, 15*time.Second)
	if v.Done() || v.Ready() != 1 {
		t.Fatalf("early verdict = %+v", v)
	}
	if _, failed := v.Failure(); failed {
		t.Fatal("nothing failed yet")
	}
	cs[1].Health = "healthy"
	cs[2].StartedAt = now.Add(-20 * time.Second)
	cs = append(cs, Container{Service: "cron", State: "running", StartedAt: now.Add(-16 * time.Second)})
	if v := Evaluate(services, cs, now, 15*time.Second); !v.Done() {
		t.Fatalf("all ready verdict = %+v", v)
	}
}

func TestEvaluateFailures(t *testing.T) {
	services := []Service{{Name: "perms", OneShot: true}, {Name: "core"}}
	v := Evaluate(services, []Container{{Service: "perms", State: "exited", ExitCode: 1}}, now, time.Second)
	if s, failed := v.Failure(); !failed || s.Name != "perms" {
		t.Fatalf("perms failure = %+v", v)
	}
	v = Evaluate(services, []Container{
		{Service: "perms", State: "exited"},
		{Service: "core", State: "restarting", RestartCount: 3, ExitCode: 1},
	}, now, time.Second)
	if s, failed := v.Failure(); !failed || s.Name != "core" || !strings.Contains(s.Detail, "3 раз") {
		t.Fatalf("crash loop = %+v", v)
	}
	v = Evaluate(services, []Container{
		{Service: "perms", State: "exited"},
		{Service: "core", State: "restarting", RestartCount: 1},
	}, now, time.Second)
	if _, failed := v.Failure(); failed || v.Done() {
		t.Fatalf("one restart is not a failure yet: %+v", v)
	}
}

func TestAtLeast(t *testing.T) {
	cases := []struct {
		v, min string
		want   bool
	}{
		{"2.24.0", "2.24.0", true},
		{"v2.29.7", "2.24.0", true},
		{"5.6.0", "2.24.0", true},
		{"2.21.0", "2.24.0", false},
		{"2.24.0-desktop.1", "2.24.0", true},
		{"1.29.2", "2.24.0", false},
	}
	for _, c := range cases {
		if got := AtLeast(c.v, c.min); got != c.want {
			t.Errorf("AtLeast(%q, %q) = %v", c.v, c.min, got)
		}
	}
}

func TestSummarizeKeepsErrors(t *testing.T) {
	in := " Image a Skipped Image is already present locally      \n Image x Pulling   \n Image x Error failed to resolve reference \"x\": not found\nError response from daemon: not found   \nError response from daemon: not found\n"
	got := summarize(in)
	want := "Image x Error failed to resolve reference \"x\": not found\nError response from daemon: not found"
	if got != want {
		t.Fatalf("summarize = %q", got)
	}
	if got := summarize("line one\nline two\n"); got != "line one\nline two" {
		t.Fatalf("plain = %q", got)
	}
}

func TestEvaluateStoppedContainer(t *testing.T) {
	v := Evaluate([]Service{{Name: "subs"}}, []Container{{Service: "subs", State: "exited", ExitCode: 137}}, now, time.Second)
	if v.Done() || v.States[0].Detail != "остановлен, код 137" {
		t.Fatalf("stopped = %+v", v)
	}
}

func TestParsePlan(t *testing.T) {
	services := []Service{{Name: "perms", OneShot: true}, {Name: "caddy"}, {Name: "bootstrap", OneShot: true}, {Name: "xray"}, {Name: "agent"}, {Name: "new-svc"}}
	out := []byte(`{"id":"Container sylph-node-xray-1","status":"Done","text":"Running"}
{"id":"Container sylph-node-caddy-1","status":"Done","text":"Running"}
{"id":"Container sylph-node-agent-1","status":"Working","text":"Recreate"}
{"id":"Container sylph-node-bootstrap-1","status":"Working","text":"Recreate"}
{"id":"Container sylph-node-new-svc-1","status":"Working","text":"Create"}
{"id":"Container 1eca61bca558_sylph-node-agent-1","status":"Working","text":"Starting"}
{"id":"Container sylph-node-perms-1","status":"Working","text":"Starting"}
{"id":"Image ghcr.io/itg-labs/sylph-agent:0.3.0","status":"Done","text":"Pulled"}
`)
	got, err := parsePlan(out, "sylph-node", services)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "agent,bootstrap,new-svc" {
		t.Fatalf("recreated = %v", names)
	}
	if got[1].OneShot != true || got[0].OneShot {
		t.Fatalf("one-shot flags lost: %+v", got)
	}
}

func TestParsePlanErrors(t *testing.T) {
	services := []Service{{Name: "caddy"}}
	out := []byte(`{"id":"Container sylph-node-caddy-1","status":"Working","text":"Recreate"}
{"id":"Container sylph-node-caddy-1","status":"Error","text":"Error response from daemon: No such image: x"}
{"error":true,"message":"Error response from daemon: No such image: x"}
`)
	if _, err := parsePlan(out, "sylph-node", services); err == nil || !strings.Contains(err.Error(), "No such image") {
		t.Fatalf("err = %v", err)
	}
	if _, err := parsePlan([]byte(" Container sylph-node-caddy-1 Recreate \n"), "sylph-node", services); err == nil {
		t.Fatal("plain progress output must not pass as an empty plan")
	}
}

func TestParseProjectName(t *testing.T) {
	name, svcs, err := parseProject([]byte(`{"name":"sylph-node","services":{"caddy":{"restart":"unless-stopped"}}}`))
	if err != nil || name != "sylph-node" || len(svcs) != 1 || svcs[0].OneShot {
		t.Fatalf("%q %+v %v", name, svcs, err)
	}
}

func TestFilesArgs(t *testing.T) {
	f := Files{ProjectDir: "/root/sylph-node", EnvFile: "/tmp/s/.env", Files: []string{"/tmp/s/compose.yml", "/root/sylph-node/compose.override.yml"}}
	got := strings.Join(f.flags(), " ")
	if got != "--project-directory /root/sylph-node --env-file /tmp/s/.env -f /tmp/s/compose.yml -f /root/sylph-node/compose.override.yml" {
		t.Fatal(got)
	}
}
