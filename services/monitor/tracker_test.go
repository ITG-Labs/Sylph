package monitor

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func keys(alerts []Alert) []string {
	var out []string
	for _, a := range alerts {
		out = append(out, a.Kind+" "+a.Problem.Key)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTrackerFiresAfterDelayAndResolvesOnce(t *testing.T) {
	tr := NewTracker()
	node := Problem{Key: "node:1:offline", Category: "node", Level: 2, Text: "Нода лежит", DelaySec: 120}
	money := Problem{Key: "money:mismatch", Category: "money", Level: 2, Text: "mismatch: 1"}
	if got := keys(tr.Step(t0, []Problem{node, money})); !eq(got, []string{"problem money:mismatch"}) {
		t.Fatalf("first step = %v", got)
	}
	if got := keys(tr.Step(t0.Add(time.Minute), []Problem{node, money})); len(got) != 0 {
		t.Fatalf("before delay = %v", got)
	}
	if got := keys(tr.Step(t0.Add(2*time.Minute), []Problem{node, money})); !eq(got, []string{"problem node:1:offline"}) {
		t.Fatalf("after delay = %v", got)
	}
	if got := keys(tr.Step(t0.Add(3*time.Minute), []Problem{node, money})); len(got) != 0 {
		t.Fatalf("repeat = %v", got)
	}
	if got := keys(tr.Step(t0.Add(4*time.Minute), nil)); !eq(got, []string{"resolved money:mismatch", "resolved node:1:offline"}) {
		t.Fatalf("resolve = %v", got)
	}
	if got := keys(tr.Step(t0.Add(5*time.Minute), nil)); len(got) != 0 {
		t.Fatalf("resolved twice = %v", got)
	}
}

func TestTrackerDropsFlapsSilently(t *testing.T) {
	tr := NewTracker()
	node := Problem{Key: "node:1:offline", Level: 2, DelaySec: 120}
	tr.Step(t0, []Problem{node})
	if got := keys(tr.Step(t0.Add(time.Minute), nil)); len(got) != 0 {
		t.Fatalf("flap resolved loudly: %v", got)
	}
	if got := keys(tr.Step(t0.Add(2*time.Minute), []Problem{node})); len(got) != 0 {
		t.Fatalf("delay restarted? got %v", got)
	}
}

func TestTrackerReAlertsOnlyWhenLevelRises(t *testing.T) {
	tr := NewTracker()
	cert := Problem{Key: "cert:bot", Level: 1, Text: "через 20 дн."}
	tr.Step(t0, []Problem{cert})
	cert.Text = "через 19 дн."
	if got := keys(tr.Step(t0.Add(24*time.Hour), []Problem{cert})); len(got) != 0 {
		t.Fatalf("text change re-alerted: %v", got)
	}
	cert.Level, cert.Text = 2, "через 6 дн."
	got := tr.Step(t0.Add(14*24*time.Hour), []Problem{cert})
	if len(got) != 1 || got[0].Kind != KindProblem || got[0].Problem.Text != "через 6 дн." {
		t.Fatalf("escalation = %+v", got)
	}
	cert.Level = 1
	if got := keys(tr.Step(t0.Add(15*24*time.Hour), []Problem{cert})); len(got) != 0 {
		t.Fatalf("level drop alerted: %v", got)
	}
}

func TestTrackerCoreUnreachableKeepsOtherProblems(t *testing.T) {
	tr := NewTracker()
	money := Problem{Key: "money:mismatch", Level: 2}
	tr.Step(t0, []Problem{money})
	if got := keys(tr.CoreDown(t0.Add(time.Minute))); len(got) != 0 {
		t.Fatalf("core down fired before delay: %v", got)
	}
	if got := keys(tr.CoreDown(t0.Add(3 * time.Minute))); !eq(got, []string{"problem " + CoreUnreachableKey}) {
		t.Fatalf("core down = %v", got)
	}
	if got := keys(tr.Step(t0.Add(4*time.Minute), []Problem{money})); !eq(got, []string{"resolved " + CoreUnreachableKey}) {
		t.Fatalf("core back = %v", got)
	}
}

func TestTrackerUnsentAlertsAreRetried(t *testing.T) {
	tr := NewTracker()
	p := Problem{Key: "money:mismatch", Level: 2}
	alerts := tr.Step(t0, []Problem{p})
	tr.Failed(alerts[0])
	if got := keys(tr.Step(t0.Add(time.Minute), []Problem{p})); !eq(got, []string{"problem money:mismatch"}) {
		t.Fatalf("retry = %v", got)
	}
	resolved := tr.Step(t0.Add(2*time.Minute), nil)
	tr.Failed(resolved[0])
	if got := keys(tr.Step(t0.Add(3*time.Minute), nil)); !eq(got, []string{"resolved money:mismatch"}) {
		t.Fatalf("resolve retry = %v", got)
	}
}

func TestTrackerRoundTripsThroughJSON(t *testing.T) {
	tr := NewTracker()
	tr.Step(t0, []Problem{{Key: "money:mismatch", Level: 2, Text: "x"}})
	data, err := tr.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	back := NewTracker()
	if err := back.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if got := keys(back.Step(t0.Add(time.Minute), []Problem{{Key: "money:mismatch", Level: 2}})); len(got) != 0 {
		t.Fatalf("restored tracker re-alerted: %v", got)
	}
	if got := keys(back.Step(t0.Add(2*time.Minute), nil)); !eq(got, []string{"resolved money:mismatch"}) {
		t.Fatalf("restored tracker forgot to resolve: %v", got)
	}
}

func TestTrackerDelayToleratesTickJitter(t *testing.T) {
	tr := NewTracker()
	node := Problem{Key: "node:1:offline", Level: 2, DelaySec: 120}
	tr.Step(t0.Add(400*time.Millisecond), []Problem{node})
	if got := keys(tr.Step(t0.Add(time.Minute+300*time.Millisecond), []Problem{node})); len(got) != 0 {
		t.Fatalf("fired after one tick: %v", got)
	}
	if got := keys(tr.Step(t0.Add(2*time.Minute+300*time.Millisecond), []Problem{node})); !eq(got, []string{"problem node:1:offline"}) {
		t.Fatalf("a tick 0.1s short of the delay must still fire: %v", got)
	}
}
