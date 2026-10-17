package monitor

import (
	"encoding/json"
	"sort"
	"time"
)

const (
	KindProblem  = "problem"
	KindResolved = "resolved"

	coreDownDelaySec = 120
)

type Alert struct {
	Kind    string
	Problem Problem
	prev    int
}

type entry struct {
	FirstSeen time.Time `json:"first_seen"`
	SentLevel int       `json:"sent_level"`
	Problem   Problem   `json:"problem"`
	Resolving bool      `json:"resolving,omitempty"`
}

type Tracker struct {
	entries map[string]*entry
}

func NewTracker() *Tracker {
	return &Tracker{entries: map[string]*entry{}}
}

func (t *Tracker) Step(now time.Time, problems []Problem) []Alert {
	present := map[string]bool{}
	var alerts []Alert
	for _, p := range problems {
		present[p.Key] = true
		alerts = append(alerts, t.observe(now, p)...)
	}
	var gone []string
	for key := range t.entries {
		if !present[key] {
			gone = append(gone, key)
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		e := t.entries[key]
		delete(t.entries, key)
		if e.SentLevel > 0 {
			alerts = append(alerts, Alert{Kind: KindResolved, Problem: e.Problem, prev: e.SentLevel})
		}
	}
	return alerts
}

func (t *Tracker) CoreDown(now time.Time) []Alert {
	return t.observe(now, Problem{Key: CoreUnreachableKey, Category: "core", Level: 2, Text: "Ядро недоступно: монитор не может получить состояние", DelaySec: coreDownDelaySec})
}

func (t *Tracker) observe(now time.Time, p Problem) []Alert {
	e := t.entries[p.Key]
	if e == nil {
		e = &entry{FirstSeen: now}
		t.entries[p.Key] = e
	}
	e.Resolving = false
	e.Problem = p
	if now.Sub(e.FirstSeen) < time.Duration(p.DelaySec)*time.Second || p.Level <= e.SentLevel {
		return nil
	}
	prev := e.SentLevel
	e.SentLevel = p.Level
	return []Alert{{Kind: KindProblem, Problem: p, prev: prev}}
}

func (t *Tracker) Failed(a Alert) {
	switch a.Kind {
	case KindProblem:
		if e := t.entries[a.Problem.Key]; e != nil {
			e.SentLevel = a.prev
		}
	case KindResolved:
		if _, back := t.entries[a.Problem.Key]; !back {
			t.entries[a.Problem.Key] = &entry{SentLevel: a.prev, Problem: a.Problem, Resolving: true}
		}
	}
}

func (t *Tracker) Open() []Problem {
	var out []Problem
	for _, e := range t.entries {
		if e.SentLevel > 0 && !e.Resolving {
			out = append(out, e.Problem)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (t *Tracker) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.entries)
}

func (t *Tracker) UnmarshalJSON(data []byte) error {
	entries := map[string]*entry{}
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	t.entries = entries
	return nil
}
