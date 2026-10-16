package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/agent/internal/xraygrpc"
)

func runLiveXray(t *testing.T, img string, config []byte) *xraygrpc.Client {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "127.0.0.1::10085", "-v", dir+":/etc/sylph:ro", img, "run", "-c", "/etc/sylph/config.json").CombinedOutput()
	if err != nil {
		t.Fatalf("start xray: %v\n%s", err, out)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	port, err := exec.Command("docker", "port", id, "10085/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	addr := strings.TrimSpace(strings.Split(string(port), "\n")[0])
	x, err := xraygrpc.Dial(addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	if !WaitForXray(context.Background(), x, 30*time.Second, 200*time.Millisecond, discardLogger()) {
		logs, _ := exec.Command("docker", "logs", id).CombinedOutput()
		t.Fatalf("xray did not start:\n%s", logs)
	}
	return x
}

func TestLivePlanRulesFollowPlansWithoutRestart(t *testing.T) {
	img := xrayImage(t)
	ctx := context.Background()
	p := planProfile("best")
	p.Rules[1].To = "block"
	config, err := BuildXrayRouted(XrayInput{Inbounds: goldenInbounds(), APIListen: "0.0.0.0:10085", Routing: p, GeoPrefix: "sylph"})
	if err != nil {
		t.Fatal(err)
	}
	x := runLiveXray(t, img, config)
	route := func(user string) string {
		t.Helper()
		out, err := x.TestRoute(ctx, "alpha", user)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	a, b := "6f1c2a3b-4d5e-4f60-8a7b-00000000000a", "6f1c2a3b-4d5e-4f60-8a7b-00000000000b"
	if route(a) == "" || route(a) == "block" {
		t.Fatalf("before plan rules a goes to %q, want the balancer", route(a))
	}
	k := NewPlanRules(x, discardLogger())
	k.Keep(ctx, p, map[int64][]string{3: {a}})
	tags, err := x.RuleTags(ctx)
	if err != nil || k.Error() != nil {
		t.Fatalf("rules: %v %v", err, k.Error())
	}
	if slices.Contains(tags, noderoute.DefaultRuleTag) || tags[len(tags)-1] != "sylph-p1-default" {
		t.Fatalf("live rule tags = %v", tags)
	}
	if route(a) != "block" || route(b) == "block" || route(b) == "" {
		t.Fatalf("plan 3: a -> %q, b -> %q", route(a), route(b))
	}
	before, err := x.Uptime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	k.Keep(ctx, p, map[int64][]string{3: {b}})
	if route(b) != "block" || route(a) == "block" || route(a) == "" || k.Error() != nil {
		t.Fatalf("after a payment: a -> %q, b -> %q, error %v", route(a), route(b), k.Error())
	}
	if tags, _ := x.RuleTags(ctx); slices.ContainsFunc(tags, func(t string) bool { return strings.HasPrefix(t, "sylph-p1-") }) {
		t.Fatalf("old generation left in xray: %v", tags)
	}
	if after, err := x.Uptime(ctx); err != nil || after < before {
		t.Fatalf("xray restarted: uptime %v -> %v (%v)", before, after, err)
	}
}
