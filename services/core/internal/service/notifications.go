package service

import (
	"context"
	"fmt"
	"math/bits"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	maxDueNotifications = 100
	familyTraffic       = "traffic"
	familyExpiry        = "expiry"
)

type notifyKind struct {
	family   string
	severity int
	urgency  int
}

var notifyKinds = map[string]notifyKind{
	"traffic_80":  {familyTraffic, 1, 6},
	"traffic_95":  {familyTraffic, 2, 4},
	"traffic_out": {familyTraffic, 3, 2},
	"expiry_3d":   {familyExpiry, 1, 7},
	"expiry_1d":   {familyExpiry, 2, 5},
	"expiry_1h":   {familyExpiry, 3, 3},
	"expired":     {familyExpiry, 4, 1},
}

var expiryThresholds = []struct {
	kind  string
	left  time.Duration
	trial bool
}{
	{"expiry_1h", time.Hour, true},
	{"expiry_1d", 24 * time.Hour, true},
	{"expiry_3d", 72 * time.Hour, false},
}

type Notification struct {
	UserID            int64      `json:"user_id"`
	TelegramID        int64      `json:"telegram_id"`
	Kind              string     `json:"kind"`
	Ref               string     `json:"ref"`
	Trial             bool       `json:"trial"`
	PaidUntil         *time.Time `json:"paid_until"`
	PeriodEnds        *time.Time `json:"period_ends"`
	TrafficUsedBytes  *int64     `json:"traffic_used_bytes"`
	TrafficLimitBytes *int64     `json:"traffic_limit_bytes"`
	PoolID            *int64     `json:"pool_id,omitempty"`
	PoolName          string     `json:"pool_name,omitempty"`
	MultiPool         bool       `json:"multi_pool"`
	TrafficExhausted  bool       `json:"traffic_exhausted"`
}

type NotificationSent struct {
	UserID  int64  `json:"user_id"`
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Outcome string `json:"outcome"`
}

func trafficRef(periodID, bonus int64) string {
	return fmt.Sprintf("%d:%d", periodID, bonus)
}

func poolRef(periodID, bonus, poolID int64) string {
	return fmt.Sprintf("%d:%d:%d", periodID, bonus, poolID)
}

func trafficKind(used, limit int64) string {
	switch {
	case used >= limit:
		return "traffic_out"
	case atLeastPercent(used, limit, 95):
		return "traffic_95"
	case atLeastPercent(used, limit, 80):
		return "traffic_80"
	}
	return ""
}

func untilRef(t time.Time) string {
	return strconv.FormatInt(t.UnixMicro(), 10)
}

func atLeastPercent(used, limit int64, pct uint64) bool {
	hu, lu := bits.Mul64(uint64(used), 100)
	hl, ll := bits.Mul64(uint64(limit), pct)
	return hu > hl || (hu == hl && lu >= ll)
}

func expiryOn(st store.Settings, trial bool) bool {
	if trial {
		return st.NotifyTrial
	}
	return st.NotifyExpiry
}

func dueFor(c store.NotifyCandidate, now time.Time, st store.Settings) []Notification {
	var out []Notification
	base := Notification{UserID: c.UserID, TelegramID: c.TelegramID}
	if st.NotifyTraffic && c.PeriodID != nil {
		traffic := base
		traffic.PeriodEnds, traffic.PaidUntil = c.PeriodEnds, c.PaidUntil
		traffic.MultiPool, traffic.TrafficExhausted = len(c.Pools) > 0, c.Exhausted
		if !c.MainIdle && c.LimitBytes != nil && *c.LimitBytes > 0 {
			used, limit := c.UsedBytes, *c.LimitBytes
			if kind := trafficKind(used, limit); kind != "" {
				n := traffic
				n.Kind, n.Ref = kind, trafficRef(*c.PeriodID, c.BonusBytes)
				n.TrafficUsedBytes, n.TrafficLimitBytes = &used, &limit
				out = append(out, n)
			}
		}
		for _, p := range c.Pools {
			if p.Limit == nil || *p.Limit <= 0 {
				continue
			}
			used, limit, id := p.Used, *p.Limit, p.ID
			if kind := trafficKind(used, limit); kind != "" {
				n := traffic
				n.Kind, n.Ref, n.PoolID, n.PoolName = kind, poolRef(*c.PeriodID, p.Bonus, p.ID), &id, p.Name
				n.TrafficUsedBytes, n.TrafficLimitBytes = &used, &limit
				out = append(out, n)
			}
		}
	}
	switch {
	case c.PeriodID != nil && c.PaidUntil != nil && c.PeriodStarts != nil:
		if !expiryOn(st, c.TailTrial) {
			break
		}
		left, chain := c.PaidUntil.Sub(now), c.PaidUntil.Sub(*c.PeriodStarts)
		for _, th := range expiryThresholds {
			if c.TailTrial && !th.trial {
				continue
			}
			if left <= th.left && chain > th.left {
				n := base
				n.Kind, n.Ref, n.Trial, n.PaidUntil = th.kind, untilRef(*c.PaidUntil), c.TailTrial, c.PaidUntil
				out = append(out, n)
				break
			}
		}
	case c.PeriodID == nil && c.EndedAt != nil:
		if expiryOn(st, c.EndedTrial) {
			n := base
			n.Kind, n.Ref, n.Trial, n.PaidUntil = "expired", untilRef(*c.EndedAt), c.EndedTrial, c.EndedAt
			out = append(out, n)
		}
	}
	return out
}

func suppressed(n Notification, marks []store.NotifyMark) bool {
	want := notifyKinds[n.Kind]
	for _, m := range marks {
		got, ok := notifyKinds[m.Kind]
		if ok && m.Ref == n.Ref && got.family == want.family && (got.severity >= want.severity || m.Outcome == "unreachable") {
			return true
		}
	}
	return false
}

func (s *Service) DueNotifications(ctx context.Context, now time.Time) ([]Notification, error) {
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := []Notification{}
	if !st.NotifyTraffic && !st.NotifyExpiry && !st.NotifyTrial {
		return out, nil
	}
	cands, err := s.store.NotifyCandidates(ctx, now)
	if err != nil {
		return nil, err
	}
	var due []Notification
	var ids []int64
	for _, c := range cands {
		if d := dueFor(c, now, st); len(d) > 0 {
			due = append(due, d...)
			ids = append(ids, c.UserID)
		}
	}
	marks, err := s.store.NotifyMarks(ctx, ids)
	if err != nil {
		return nil, err
	}
	byUser := map[int64][]store.NotifyMark{}
	for _, m := range marks {
		byUser[m.UserID] = append(byUser[m.UserID], m)
	}
	for _, n := range due {
		if !suppressed(n, byUser[n.UserID]) {
			out = append(out, n)
		}
	}
	slices.SortStableFunc(out, func(a, b Notification) int {
		return notifyKinds[a.Kind].urgency - notifyKinds[b.Kind].urgency
	})
	if len(out) > maxDueNotifications {
		out = out[:maxDueNotifications]
	}
	return out, nil
}

func (s *Service) MarkNotified(ctx context.Context, in NotificationSent) error {
	if in.UserID <= 0 {
		return fieldErr("user_id", "must be positive")
	}
	kind, ok := notifyKinds[in.Kind]
	if !ok {
		return fieldErr("kind", "unknown notification kind")
	}
	if in.Outcome != "sent" && in.Outcome != "unreachable" && in.Outcome != "failed" {
		return fieldErr("outcome", "must be sent, unreachable or failed")
	}
	m := store.NotifyMark{UserID: in.UserID, Kind: in.Kind, Ref: in.Ref, Outcome: in.Outcome}
	switch kind.family {
	case familyTraffic:
		id, ok := parseTrafficRef(in.Ref)
		if !ok {
			return fieldErr("ref", "malformed traffic reference")
		}
		m.PeriodID = &id
	default:
		micros, err := strconv.ParseInt(in.Ref, 10, 64)
		if err != nil || untilRef(time.UnixMicro(micros)) != in.Ref {
			return fieldErr("ref", "malformed expiry reference")
		}
		until := time.UnixMicro(micros)
		m.PaidUntil = &until
	}
	return s.store.MarkNotified(ctx, m)
}

func parseTrafficRef(ref string) (int64, bool) {
	parts := strings.Split(ref, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, false
	}
	nums := make([]int64, len(parts))
	for i, part := range parts {
		v, err := strconv.ParseInt(part, 10, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		nums[i] = v
	}
	if nums[0] <= 0 {
		return 0, false
	}
	if len(nums) == 2 {
		return nums[0], trafficRef(nums[0], nums[1]) == ref
	}
	return nums[0], nums[2] > 0 && poolRef(nums[0], nums[1], nums[2]) == ref
}
