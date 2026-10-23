package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

func mkPeriod(t *testing.T, s *Store, userID, planID int64, starts, expires time.Time) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(context.Background(),
		`INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, planID, starts, expires).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func candidatesByUser(t *testing.T, s *Store, now time.Time) map[int64]NotifyCandidate {
	t.Helper()
	list, err := s.NotifyCandidates(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]NotifyCandidate{}
	for _, c := range list {
		out[c.UserID] = c
	}
	return out
}

func TestNotifyCandidatesTraffic(t *testing.T) {
	s := setupStore(t)
	now := time.Now()
	plan := mkLimitedPlan(t, s, 30, 1000)
	below := tgUser(t, s, 9101)
	at80 := tgUser(t, s, 9102)
	bonus := tgUser(t, s, 9103)
	setUsed(t, s, mkPeriod(t, s, below.ID, plan.ID, now.Add(-day), now.Add(20*day)), 799)
	p80 := mkPeriod(t, s, at80.ID, plan.ID, now.Add(-day), now.Add(20*day))
	setUsed(t, s, p80, 800)
	pb := mkPeriod(t, s, bonus.ID, plan.ID, now.Add(-day), now.Add(20*day))
	setUsed(t, s, pb, 900)
	if _, err := s.pool.Exec(context.Background(), `UPDATE subscriptions SET traffic_bonus_bytes = 200 WHERE id = $1`, pb); err != nil {
		t.Fatal(err)
	}
	got := candidatesByUser(t, s, now)
	if _, ok := got[below.ID]; ok {
		t.Fatal("79.9% is a candidate")
	}
	c, ok := got[at80.ID]
	if !ok || c.PeriodID == nil || *c.PeriodID != p80 || c.UsedBytes != 800 || c.LimitBytes == nil || *c.LimitBytes != 1000 ||
		c.TelegramID != 9102 || c.PeriodEnds == nil || c.PeriodStarts == nil {
		t.Fatalf("80%% candidate = %+v (ok %v)", c, ok)
	}
	if _, ok := got[bonus.ID]; ok {
		t.Fatal("900 of 1000+200 bonus is a candidate")
	}
}

func TestNotifyCandidatesUnlimitedPlanSkipsTraffic(t *testing.T) {
	s := setupStore(t)
	now := time.Now()
	plan := &model.Plan{Name: "unlim", PriceRub: 100, PeriodDays: 30}
	if err := s.CreatePlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	u := tgUser(t, s, 9111)
	setUsed(t, s, mkPeriod(t, s, u.ID, plan.ID, now.Add(-day), now.Add(20*day)), 1<<40)
	if got := candidatesByUser(t, s, now); len(got) != 0 {
		t.Fatalf("unlimited plan gave %+v", got)
	}
}

func TestNotifyCandidatesExpiryUsesPaidUntil(t *testing.T) {
	s := setupStore(t)
	now := time.Now()
	plan := mkLimitedPlan(t, s, 30, 1<<40)
	soon := tgUser(t, s, 9121)
	far := tgUser(t, s, 9122)
	queued := tgUser(t, s, 9123)
	pSoon := mkPeriod(t, s, soon.ID, plan.ID, now.Add(-27*day), now.Add(2*day))
	mkPeriod(t, s, far.ID, plan.ID, now.Add(-day), now.Add(4*day))
	cur := mkPeriod(t, s, queued.ID, plan.ID, now.Add(-29*day), now.Add(day))
	mkPeriod(t, s, queued.ID, plan.ID, now.Add(day), now.Add(31*day))
	got := candidatesByUser(t, s, now)
	c, ok := got[soon.ID]
	if !ok || c.PeriodID == nil || *c.PeriodID != pSoon || c.PaidUntil == nil || !sameTime(*c.PaidUntil, now.Add(2*day)) ||
		!sameTime(*c.PeriodStarts, now.Add(-27*day)) || c.TailTrial {
		t.Fatalf("soon = %+v (ok %v)", c, ok)
	}
	if _, ok := got[far.ID]; ok {
		t.Fatal("4 days left is a candidate")
	}
	if _, ok := got[queued.ID]; ok {
		t.Fatalf("user with a queued period is a candidate (current %d)", cur)
	}
}

func TestNotifyCandidatesExpiredWithin24h(t *testing.T) {
	s := setupStore(t)
	now := time.Now()
	plan := mkLimitedPlan(t, s, 30, 1<<40)
	trial := mkTrialPlan(t, s, model.VisibilityPublic)
	recent := tgUser(t, s, 9131)
	old := tgUser(t, s, 9132)
	renewed := tgUser(t, s, 9133)
	trialEnded := tgUser(t, s, 9134)
	gap := tgUser(t, s, 9135)
	mkPeriod(t, s, recent.ID, plan.ID, now.Add(-30*day), now.Add(-2*time.Hour))
	mkPeriod(t, s, old.ID, plan.ID, now.Add(-31*day), now.Add(-25*time.Hour))
	mkPeriod(t, s, renewed.ID, plan.ID, now.Add(-30*day), now.Add(-2*time.Hour))
	mkPeriod(t, s, renewed.ID, plan.ID, now.Add(-time.Hour), now.Add(29*day))
	mkPeriod(t, s, trialEnded.ID, trial.ID, now.Add(-3*day), now.Add(-time.Hour))
	mkPeriod(t, s, gap.ID, plan.ID, now.Add(-30*day), now.Add(-time.Hour))
	mkPeriod(t, s, gap.ID, plan.ID, now.Add(time.Hour), now.Add(30*day))
	got := candidatesByUser(t, s, now)
	c, ok := got[recent.ID]
	if !ok || c.PeriodID != nil || c.PaidUntil != nil || c.EndedAt == nil || !sameTime(*c.EndedAt, now.Add(-2*time.Hour)) || c.EndedTrial {
		t.Fatalf("recent = %+v (ok %v)", c, ok)
	}
	if _, ok := got[old.ID]; ok {
		t.Fatal("ended 25h ago is a candidate")
	}
	if _, ok := got[renewed.ID]; ok {
		t.Fatal("renewed user is a candidate")
	}
	if c, ok := got[trialEnded.ID]; !ok || !c.EndedTrial {
		t.Fatalf("trial ended = %+v (ok %v)", c, ok)
	}
	if _, ok := got[gap.ID]; ok {
		t.Fatal("user with a future period is reported as expired")
	}
}

func TestNotifyCandidatesSkipCanceledBannedAndNoTelegram(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	now := time.Now()
	plan := mkLimitedPlan(t, s, 30, 1<<40)
	closed := tgUser(t, s, 9141)
	banned := tgUser(t, s, 9142)
	noTG := &model.User{SubToken: model.NewToken(), Status: "active"}
	if err := s.CreateUser(ctx, noTG); err != nil {
		t.Fatal(err)
	}
	pc := mkPeriod(t, s, closed.ID, plan.ID, now.Add(-day), now.Add(time.Hour))
	setUsed(t, s, pc, 1<<40)
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET canceled = TRUE WHERE id = $1`, pc); err != nil {
		t.Fatal(err)
	}
	mkPeriod(t, s, banned.ID, plan.ID, now.Add(-day), now.Add(time.Hour))
	if _, err := s.pool.Exec(ctx, `UPDATE users SET status = 'banned' WHERE id = $1`, banned.ID); err != nil {
		t.Fatal(err)
	}
	mkPeriod(t, s, noTG.ID, plan.ID, now.Add(-day), now.Add(time.Hour))
	if got := candidatesByUser(t, s, now); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestNotifyCandidatesTailTrial(t *testing.T) {
	s := setupStore(t)
	now := time.Now()
	trial := mkTrialPlan(t, s, model.VisibilityPublic)
	u := tgUser(t, s, 9151)
	mkPeriod(t, s, u.ID, trial.ID, now.Add(-2*day), now.Add(20*time.Hour))
	c, ok := candidatesByUser(t, s, now)[u.ID]
	if !ok || !c.TailTrial || c.LimitBytes != nil {
		t.Fatalf("trial = %+v (ok %v)", c, ok)
	}
}

func TestMarkNotified(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	u := tgUser(t, s, 9161)
	until := time.Now().Add(day).Truncate(time.Microsecond)
	m := NotifyMark{UserID: u.ID, Kind: "expiry_1d", Ref: "r1", PaidUntil: &until, Outcome: "sent"}
	if err := s.MarkNotified(ctx, m); err != nil {
		t.Fatal(err)
	}
	dup := m
	dup.Outcome = "unreachable"
	if err := s.MarkNotified(ctx, dup); err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	marks, err := s.NotifyMarks(ctx, []int64{u.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 1 || marks[0].Kind != "expiry_1d" || marks[0].Ref != "r1" || marks[0].Outcome != "sent" ||
		marks[0].PaidUntil == nil || !marks[0].PaidUntil.Equal(until) || marks[0].PeriodID != nil {
		t.Fatalf("marks = %+v", marks)
	}
	missing := m
	missing.UserID = u.ID + 1000
	if err := s.MarkNotified(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	if marks, err := s.NotifyMarks(ctx, nil); err != nil || marks != nil {
		t.Fatalf("empty ids: %v %v", marks, err)
	}
}

func TestCleanupKeepsLiveNotificationMarks(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	now := time.Now()
	plan := mkLimitedPlan(t, s, 30, 1000)
	u := tgUser(t, s, 9171)
	live := mkPeriod(t, s, u.ID, plan.ID, now.Add(-40*day), now.Add(300*day))
	gone := mkPeriod(t, s, u.ID, plan.ID, now.Add(-80*day), now.Add(-50*day))
	soon := now.Add(2 * day)
	passed := now.Add(-2 * day)
	recent := now.Add(-12 * time.Hour)
	for _, m := range []NotifyMark{
		{UserID: u.ID, Kind: "traffic_80", Ref: "live", PeriodID: &live, Outcome: "sent"},
		{UserID: u.ID, Kind: "traffic_80", Ref: "gone", PeriodID: &gone, Outcome: "sent"},
		{UserID: u.ID, Kind: "expiry_3d", Ref: "soon", PaidUntil: &soon, Outcome: "sent"},
		{UserID: u.ID, Kind: "expired", Ref: "passed", PaidUntil: &passed, Outcome: "sent"},
		{UserID: u.ID, Kind: "expired", Ref: "recent", PaidUntil: &recent, Outcome: "sent"},
	} {
		if err := s.MarkNotified(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE notifications_sent SET created_at = now() - interval '60 days'`); err != nil {
		t.Fatal(err)
	}
	res, err := s.CleanupHistory(ctx, now.Add(-30*day), 100)
	if err != nil {
		t.Fatal(err)
	}
	if res.Notifications != 2 {
		t.Fatalf("deleted %d marks", res.Notifications)
	}
	marks, err := s.NotifyMarks(ctx, []int64{u.ID})
	if err != nil {
		t.Fatal(err)
	}
	left := map[string]bool{}
	for _, m := range marks {
		left[m.Ref] = true
	}
	if !left["live"] || !left["soon"] || !left["recent"] || left["gone"] || left["passed"] {
		t.Fatalf("left %v", left)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE notifications_sent SET created_at = now()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE subscriptions SET expires_at = now() - interval '1 day' WHERE id = $1`, live); err != nil {
		t.Fatal(err)
	}
	if res, err := s.CleanupHistory(ctx, now.Add(-30*day), 100); err != nil || res.Notifications != 0 {
		t.Fatalf("fresh marks deleted: %+v %v", res, err)
	}
}

func TestSettingsNotifyToggles(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	st, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.NotifyTraffic || !st.NotifyExpiry || !st.NotifyTrial {
		t.Fatalf("defaults = %+v", st)
	}
	off := false
	st, err = s.UpdateSettings(ctx, SettingsPatch{NotifyExpiry: &off})
	if err != nil {
		t.Fatal(err)
	}
	if !st.NotifyTraffic || st.NotifyExpiry || !st.NotifyTrial || st.SubsTitle != "Sylph" {
		t.Fatalf("after expiry off = %+v", st)
	}
	st, err = s.UpdateSettings(ctx, SettingsPatch{NotifyTraffic: &off, NotifyTrial: &off})
	if err != nil {
		t.Fatal(err)
	}
	if st.NotifyTraffic || st.NotifyExpiry || st.NotifyTrial {
		t.Fatalf("all off = %+v", st)
	}
}

func TestNotifyCandidatesExpiredAfterCronButNotClosedByHand(t *testing.T) {
	s := setupStore(t)
	ctx := context.Background()
	now := time.Now()
	plan := mkLimitedPlan(t, s, 30, 1<<40)
	natural := tgUser(t, s, 9181)
	closed := tgUser(t, s, 9182)
	banned := tgUser(t, s, 9183)
	mkPeriod(t, s, natural.ID, plan.ID, now.Add(-30*day), now.Add(-2*time.Hour))
	pc := mkPeriod(t, s, closed.ID, plan.ID, now.Add(-10*day), now.Add(20*day))
	mkPeriod(t, s, banned.ID, plan.ID, now.Add(-10*day), now.Add(20*day))
	if n, err := s.ExpireOverdue(ctx, now); err != nil || n != 1 {
		t.Fatalf("expire = %d, %v", n, err)
	}
	if err := s.ClosePeriod(ctx, pc, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BanUser(ctx, banned.ID, "", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UnbanUser(ctx, banned.ID); err != nil {
		t.Fatal(err)
	}
	got := candidatesByUser(t, s, now)
	if c, ok := got[natural.ID]; !ok || c.EndedAt == nil || !sameTime(*c.EndedAt, now.Add(-2*time.Hour)) {
		t.Fatalf("naturally expired after cron = %+v (ok %v)", c, ok)
	}
	if c, ok := got[closed.ID]; ok {
		t.Fatalf("closed by hand = %+v", c)
	}
	if c, ok := got[banned.ID]; ok {
		t.Fatalf("banned and unbanned = %+v", c)
	}
}
