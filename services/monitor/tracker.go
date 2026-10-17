package monitor

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const (
	KindProblem  = "problem"
	KindResolved = "resolved"

	coreDownDelaySec = 120
	delaySlack       = 5 * time.Second
)

type Alert struct {
	Kind      string
	Problem   Problem
	FirstSeen time.Time
	prev      int
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
	var held []string
	var alerts []Alert
	for _, p := range problems {
		present[p.Key] = true
		if p.Suppresses != "" {
			held = append(held, p.Suppresses)
		}
		alerts = append(alerts, t.observe(now, p)...)
	}
	var gone []string
	for key := range t.entries {
		if !present[key] && !heldBy(key, held) {
			gone = append(gone, key)
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		e := t.entries[key]
		delete(t.entries, key)
		if e.SentLevel > 0 {
			alerts = append(alerts, Alert{Kind: KindResolved, Problem: e.Problem, FirstSeen: e.FirstSeen, prev: e.SentLevel})
		}
	}
	return alerts
}

func heldBy(key string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func (t *Tracker) CoreDown(now time.Time, cause string) []Alert {
	return t.observe(now, Problem{Key: CoreUnreachableKey, Category: "core", Level: 2, Text: "Ядро недоступно: " + cause, DelaySec: coreDownDelaySec})
}

func (t *Tracker) observe(now time.Time, p Problem) []Alert {
	e := t.entries[p.Key]
	if e == nil {
		e = &entry{FirstSeen: now}
		t.entries[p.Key] = e
	}
	e.Resolving = false
	e.Problem = p
	if now.Sub(e.FirstSeen)+delaySlack < time.Duration(p.DelaySec)*time.Second || p.Level <= e.SentLevel {
		return nil
	}
	prev := e.SentLevel
	e.SentLevel = p.Level
	return []Alert{{Kind: KindProblem, Problem: p, FirstSeen: e.FirstSeen, prev: prev}}
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

func (t *Tracker) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.entries)
}

func (t *Tracker) UnmarshalJSON(data []byte) error {
	entries := map[string]*entry{}
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	for key, e := range entries {
		if e == nil {
			delete(entries, key)
		}
	}
	t.entries = entries
	return nil
}
