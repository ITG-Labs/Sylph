package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const day = 24 * time.Hour

var allOn = store.Settings{NotifyTraffic: true, NotifyExpiry: true, NotifyTrial: true}

func i64(v int64) *int64 { return &v }

func at(t time.Time) *time.Time { return &t }

func kinds(ns []Notification) []string {
	out := []string{}
	for _, n := range ns {
		out = append(out, n.Kind)
	}
	return out
}

func sameKinds(got []Notification, want ...string) bool {
	k := kinds(got)
	if len(k) != len(want) {
		return false
	}
	for i := range k {
		if k[i] != want[i] {
			return false
		}
	}
	return true
}

func trafficCand(used, limit int64) store.NotifyCandidate {
	now := time.Now()
	return store.NotifyCandidate{UserID: 1, TelegramID: 10, PeriodID: i64(7), PeriodStarts: at(now.Add(-day)),
		PeriodEnds: at(now.Add(20 * day)), PaidUntil: at(now.Add(20 * day)), UsedBytes: used, LimitBytes: &limit}
}

func TestDueForTrafficBoundaries(t *testing.T) {
	now := time.Now()
	cases := []struct {
		used, limit int64
		want        []string
	}{
		{799, 1000, nil},
		{800, 1000, []string{"traffic_80"}},
		{949, 1000, []string{"traffic_80"}},
		{950, 1000, []string{"traffic_95"}},
		{999, 1000, []string{"traffic_95"}},
		{1000, 1000, []string{"traffic_out"}},
		{2000, 1000, []string{"traffic_out"}},
		{math.MaxInt64 / 10 * 8, math.MaxInt64 / 10 * 10, []string{"traffic_80"}},
		{math.MaxInt64 - 1, math.MaxInt64, []string{"traffic_95"}},
	}
	for _, c := range cases {
		got := dueFor(trafficCand(c.used, c.limit), now, allOn)
		if !sameKinds(got, c.want...) {
			t.Errorf("%d/%d = %v, want %v", c.used, c.limit, kinds(got), c.want)
		}
	}
	n := dueFor(trafficCand(800, 1000), now, allOn)[0]
	if n.Ref != "7:0" || *n.TrafficUsedBytes != 800 || *n.TrafficLimitBytes != 1000 || n.PeriodEnds == nil || n.PaidUntil == nil || n.UserID != 1 || n.TelegramID != 10 {
		t.Fatalf("traffic notification = %+v", n)
	}
	off := allOn
	off.NotifyTraffic = false
	if got := dueFor(trafficCand(1000, 1000), now, off); len(got) != 0 {
		t.Fatalf("traffic off: %v", kinds(got))
	}
}

func expiryCand(now time.Time, started, left time.Duration, trial bool) store.NotifyCandidate {
	return store.NotifyCandidate{UserID: 1, TelegramID: 10, PeriodID: i64(7), PeriodStarts: at(now.Add(-started)),
		PeriodEnds: at(now.Add(left)), PaidUntil: at(now.Add(left)), TailTrial: trial}
}

func TestDueForExpiryThresholds(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name          string
		started, left time.Duration
		trial         bool
		want          []string
	}{
		{"4 days left", 26 * day, 4 * day, false, nil},
		{"3 days left", 27 * day, 3 * day, false, []string{"expiry_3d"}},
		{"2 days left", 28 * day, 2 * day, false, []string{"expiry_3d"}},
		{"1 day left", 29 * day, day, false, []string{"expiry_1d"}},
		{"2 hours left", 30 * day, 2 * time.Hour, false, []string{"expiry_1d"}},
		{"1 hour left", 30 * day, time.Hour, false, []string{"expiry_1h"}},
		{"bought 3-day plan", 0, 3 * day, false, nil},
		{"3-day plan, 2 days left", day, 2 * day, false, nil},
		{"3-day plan, 1 day left", 2 * day, day, false, []string{"expiry_1d"}},
		{"trial skips 3 days", day, 2 * day, true, nil},
		{"7-day trial skips 3 days", 5 * day, 2 * day, true, nil},
		{"trial 1 day left", 2 * day, 20 * time.Hour, true, []string{"expiry_1d"}},
		{"1-day trial waits for the hour", 4 * time.Hour, 20 * time.Hour, true, nil},
		{"1-day trial last hour", 23 * time.Hour, time.Hour, true, []string{"expiry_1h"}},
	}
	for _, c := range cases {
		got := dueFor(expiryCand(now, c.started, c.left, c.trial), now, allOn)
		if !sameKinds(got, c.want...) {
			t.Errorf("%s = %v, want %v", c.name, kinds(got), c.want)
			continue
		}
		if len(got) == 1 && (got[0].Trial != c.trial || got[0].Ref != untilRef(now.Add(c.left)) || !got[0].PaidUntil.Equal(now.Add(c.left))) {
			t.Errorf("%s = %+v", c.name, got[0])
		}
	}
}

func TestDueForExpiryToggles(t *testing.T) {
	now := time.Now()
	paid := expiryCand(now, 29*day, day, false)
	trial := expiryCand(now, 2*day, 20*time.Hour, true)
	noExpiry := allOn
	noExpiry.NotifyExpiry = false
	noTrial := allOn
	noTrial.NotifyTrial = false
	if got := dueFor(paid, now, noExpiry); len(got) != 0 {
		t.Fatalf("expiry off, paid: %v", kinds(got))
	}
	if got := dueFor(trial, now, noExpiry); !sameKinds(got, "expiry_1d") {
		t.Fatalf("expiry off, trial: %v", kinds(got))
	}
	if got := dueFor(trial, now, noTrial); len(got) != 0 {
		t.Fatalf("trial off, trial: %v", kinds(got))
	}
	if got := dueFor(paid, now, noTrial); !sameKinds(got, "expiry_1d") {
		t.Fatalf("trial off, paid: %v", kinds(got))
	}
}

func TestDueForExpired(t *testing.T) {
	now := time.Now()
	ended := now.Add(-2 * time.Hour)
	c := store.NotifyCandidate{UserID: 1, TelegramID: 10, EndedAt: &ended}
	got := dueFor(c, now, allOn)
	if !sameKinds(got, "expired") || got[0].Trial || got[0].Ref != untilRef(ended) || !got[0].PaidUntil.Equal(ended) {
		t.Fatalf("expired = %+v", got)
	}
	c.EndedTrial = true
	if got := dueFor(c, now, allOn); !sameKinds(got, "expired") || !got[0].Trial {
		t.Fatalf("trial expired = %+v", got)
	}
	noTrial := allOn
	noTrial.NotifyTrial = false
	if got := dueFor(c, now, noTrial); len(got) != 0 {
		t.Fatalf("trial off: %v", kinds(got))
	}
}

func TestDueForTrafficAndExpiryTogether(t *testing.T) {
	now := time.Now()
	c := expiryCand(now, 29*day, time.Hour, false)
	c.UsedBytes, c.LimitBytes = 1000, i64(1000)
	if got := dueFor(c, now, allOn); !sameKinds(got, "traffic_out", "expiry_1h") {
		t.Fatalf("both = %v", kinds(got))
	}
}

func notifyFixture(t *testing.T) (*Service, *model.Plan, *model.User) {
	t.Helper()
	svc := setupService(t)
	limit := int64(1000)
	plan := &model.Plan{Name: "month", PriceRub: 100, PeriodDays: 30, TrafficLimitBytes: &limit}
	if err := svc.store.CreatePlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	u, _, err := svc.store.RegisterTelegramUser(context.Background(), 777, model.NewToken(), model.NewUUID())
	if err != nil {
		t.Fatal(err)
	}
	return svc, plan, u
}

func insertPeriodAt(t *testing.T, svc *Service, userID, planID int64, starts, expires time.Time, used int64) int64 {
	t.Helper()
	var id int64
	if err := poolOf(svc).QueryRow(context.Background(),
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at, traffic_used_bytes) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		userID, planID, starts, expires, used).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func due(t *testing.T, svc *Service, now time.Time) []Notification {
	t.Helper()
	got, err := svc.DueNotifications(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mark(t *testing.T, svc *Service, n Notification, outcome string) {
	t.Helper()
	if err := svc.MarkNotified(context.Background(), NotificationSent{UserID: n.UserID, Kind: n.Kind, Ref: n.Ref, Outcome: outcome}); err != nil {
		t.Fatal(err)
	}
}

func TestDueNotificationsOnlyMoreUrgentAfterMark(t *testing.T) {
	svc, plan, u := notifyFixture(t)
	ctx := context.Background()
	now := time.Now()
	period := insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(-27*day), now.Add(2*day), 850)
	got := due(t, svc, now)
	if !sameKinds(got, "traffic_80", "expiry_3d") || got[0].TelegramID != 777 {
		t.Fatalf("first = %+v", got)
	}
	for _, n := range got {
		mark(t, svc, n, "sent")
	}
	if got := due(t, svc, now); len(got) != 0 {
		t.Fatalf("after marks = %v", kinds(got))
	}
	if _, err := poolOf(svc).Exec(ctx, `UPDATE subscriptions SET traffic_used_bytes = 1000 WHERE id = $1`, period); err != nil {
		t.Fatal(err)
	}
	got = due(t, svc, now.Add(36*time.Hour))
	if !sameKinds(got, "traffic_out", "expiry_1d") {
		t.Fatalf("later = %v", kinds(got))
	}
	mark(t, svc, got[0], "unreachable")
	if got := due(t, svc, now.Add(36*time.Hour)); !sameKinds(got, "expiry_1d") {
		t.Fatalf("after unreachable = %v", kinds(got))
	}
}

func TestDueNotificationsSkipLessUrgentAfterJump(t *testing.T) {
	svc, plan, u := notifyFixture(t)
	now := time.Now()
	insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(-29*day), now.Add(30*time.Minute), 0)
	got := due(t, svc, now)
	if !sameKinds(got, "expiry_1h") {
		t.Fatalf("jump = %v", kinds(got))
	}
	mark(t, svc, got[0], "sent")
	if got := due(t, svc, now); len(got) != 0 {
		t.Fatalf("less urgent came back: %v", kinds(got))
	}
}

func TestDueNotificationsNewPointAfterBonusAndRenewal(t *testing.T) {
	svc, plan, u := notifyFixture(t)
	ctx := context.Background()
	now := time.Now()
	period := insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(-27*day), now.Add(2*day), 850)
	for _, n := range due(t, svc, now) {
		mark(t, svc, n, "sent")
	}
	if _, err := poolOf(svc).Exec(ctx, `UPDATE subscriptions SET traffic_bonus_bytes = 50 WHERE id = $1`, period); err != nil {
		t.Fatal(err)
	}
	got := due(t, svc, now)
	if !sameKinds(got, "traffic_80") || got[0].Ref != trafficRef(period, 50) {
		t.Fatalf("after bonus = %+v", got)
	}
	mark(t, svc, got[0], "sent")
	insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(2*day), now.Add(32*day), 0)
	if got := due(t, svc, now); len(got) != 0 {
		t.Fatalf("renewed user still warned: %v", kinds(got))
	}
	got = due(t, svc, now.Add(31*day))
	if !sameKinds(got, "expiry_1d") || got[0].Ref != untilRef(now.Add(32*day)) {
		t.Fatalf("after renewal = %+v", got)
	}
}

func TestDueNotificationsExpiredFollowsLastHour(t *testing.T) {
	svc, plan, u := notifyFixture(t)
	now := time.Now()
	insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(-29*day), now.Add(30*time.Minute), 0)
	mark(t, svc, due(t, svc, now)[0], "sent")
	got := due(t, svc, now.Add(time.Hour))
	if !sameKinds(got, "expired") {
		t.Fatalf("after end = %v", kinds(got))
	}
	mark(t, svc, got[0], "sent")
	if got := due(t, svc, now.Add(2*time.Hour)); len(got) != 0 {
		t.Fatalf("expired twice: %v", kinds(got))
	}
	if got := due(t, svc, now.Add(26*time.Hour)); len(got) != 0 {
		t.Fatalf("after the window: %v", kinds(got))
	}
}

func TestDueNotificationsAllOffAndOrder(t *testing.T) {
	svc, plan, u := notifyFixture(t)
	ctx := context.Background()
	now := time.Now()
	insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(-27*day), now.Add(2*day), 960)
	other, _, err := svc.store.RegisterTelegramUser(ctx, 778, model.NewToken(), model.NewUUID())
	if err != nil {
		t.Fatal(err)
	}
	insertPeriodAt(t, svc, other.ID, plan.ID, now.Add(-30*day), now.Add(-time.Hour), 0)
	if got := due(t, svc, now); !sameKinds(got, "expired", "traffic_95", "expiry_3d") {
		t.Fatalf("order = %v", kinds(got))
	}
	off := false
	if _, err := svc.UpdateSettings(ctx, SettingsPatch{NotifyTraffic: &off, NotifyExpiry: &off, NotifyTrial: &off}); err != nil {
		t.Fatal(err)
	}
	got := due(t, svc, now)
	if got == nil || len(got) != 0 {
		t.Fatalf("all off = %#v", got)
	}
}

func TestMarkNotifiedValidation(t *testing.T) {
	svc, _, u := notifyFixture(t)
	ctx := context.Background()
	until := untilRef(time.Now().Add(day))
	for _, in := range []NotificationSent{
		{UserID: 0, Kind: "expired", Ref: until, Outcome: "sent"},
		{UserID: u.ID, Kind: "traffic_50", Ref: "1:2", Outcome: "sent"},
		{UserID: u.ID, Kind: "expired", Ref: until, Outcome: "maybe"},
		{UserID: u.ID, Kind: "traffic_80", Ref: "", Outcome: "sent"},
		{UserID: u.ID, Kind: "traffic_80", Ref: "7", Outcome: "sent"},
		{UserID: u.ID, Kind: "traffic_80", Ref: "7:-1", Outcome: "sent"},
		{UserID: u.ID, Kind: "traffic_80", Ref: "07:100", Outcome: "sent"},
		{UserID: u.ID, Kind: "traffic_80", Ref: "-1:100", Outcome: "sent"},
		{UserID: u.ID, Kind: "expiry_1d", Ref: "", Outcome: "sent"},
		{UserID: u.ID, Kind: "expiry_1d", Ref: "7:100", Outcome: "sent"},
		{UserID: u.ID, Kind: "expiry_1d", Ref: "+5", Outcome: "sent"},
	} {
		var fe *FieldError
		if err := svc.MarkNotified(ctx, in); !errors.As(err, &fe) {
			t.Errorf("%+v: %v", in, err)
		}
	}
	if err := svc.MarkNotified(ctx, NotificationSent{UserID: u.ID + 100, Kind: "expired", Ref: until, Outcome: "sent"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	if err := svc.MarkNotified(ctx, NotificationSent{UserID: u.ID, Kind: "traffic_80", Ref: "7:100", Outcome: "unreachable"}); err != nil {
		t.Fatalf("valid traffic: %v", err)
	}
	if err := svc.MarkNotified(ctx, NotificationSent{UserID: u.ID, Kind: "expired", Ref: until, Outcome: "failed"}); err != nil {
		t.Fatalf("failed outcome: %v", err)
	}
}

func TestDueNotificationsExpiredSurvivesCron(t *testing.T) {
	svc, plan, u := notifyFixture(t)
	ctx := context.Background()
	now := time.Now()
	insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(-29*day), now.Add(30*time.Minute), 0)
	mark(t, svc, due(t, svc, now)[0], "sent")
	later := now.Add(time.Hour)
	if n, err := svc.CheckAndExpire(ctx, later); err != nil || n != 1 {
		t.Fatalf("expire = %d, %v", n, err)
	}
	if got := due(t, svc, later); !sameKinds(got, "expired") {
		t.Fatalf("after cron = %v", kinds(got))
	}
}

func TestDueNotificationsUnreachableSilencesThePoint(t *testing.T) {
	svc, plan, u := notifyFixture(t)
	now := time.Now()
	insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(-27*day), now.Add(2*day), 0)
	mark(t, svc, due(t, svc, now)[0], "unreachable")
	if got := due(t, svc, now.Add(36*time.Hour)); len(got) != 0 {
		t.Fatalf("unreachable user warned again: %v", kinds(got))
	}
	insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(2*day), now.Add(32*day), 0)
	if got := due(t, svc, now.Add(31*day)); !sameKinds(got, "expiry_1d") {
		t.Fatalf("new point after renewal = %v", kinds(got))
	}
}

func TestDueNotificationsPlanLimitEditKeepsThePoint(t *testing.T) {
	svc, plan, u := notifyFixture(t)
	ctx := context.Background()
	now := time.Now()
	insertPeriodAt(t, svc, u.ID, plan.ID, now.Add(-day), now.Add(20*day), 960)
	got := due(t, svc, now)
	if !sameKinds(got, "traffic_95") {
		t.Fatalf("first = %v", kinds(got))
	}
	mark(t, svc, got[0], "sent")
	if _, err := poolOf(svc).Exec(ctx, `UPDATE plans SET traffic_limit_bytes = 1100 WHERE id = $1`, plan.ID); err != nil {
		t.Fatal(err)
	}
	if got := due(t, svc, now); len(got) != 0 {
		t.Fatalf("raised plan limit warned again: %v", kinds(got))
	}
	other, _, err := svc.store.RegisterTelegramUser(ctx, 779, model.NewToken(), model.NewUUID())
	if err != nil {
		t.Fatal(err)
	}
	insertPeriodAt(t, svc, other.ID, plan.ID, now.Add(-day), now.Add(20*day), 500)
	if got := due(t, svc, now); len(got) != 0 {
		t.Fatalf("45%% user warned: %v", kinds(got))
	}
	if _, err := poolOf(svc).Exec(ctx, `UPDATE plans SET traffic_limit_bytes = 600 WHERE id = $1`, plan.ID); err != nil {
		t.Fatal(err)
	}
	got = due(t, svc, now)
	if len(got) != 2 || got[0].UserID != u.ID || got[0].Kind != "traffic_out" || got[1].UserID != other.ID || got[1].Kind != "traffic_80" {
		t.Fatalf("after lowering = %+v", got)
	}
}
