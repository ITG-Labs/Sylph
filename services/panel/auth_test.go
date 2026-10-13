package panel

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func testAdmin(t *testing.T, name, password string) *admin {
	t.Helper()
	h, err := hashPassword(password, []byte("0123456789abcdef"), 1000)
	if err != nil {
		t.Fatal(err)
	}
	ph, _ := parsePasswordHash(h)
	return &admin{name: name, password: ph, secret: rfcSecret}
}

func newTestAuth(t *testing.T) (*Auth, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(1111111111, 0)}
	a := NewAuth(map[string]*admin{
		"Иван": testAdmin(t, "Иван", "secret-1"),
		"Олег": testAdmin(t, "Олег", "secret-2"),
	}, 12*time.Hour)
	a.now = clock.now
	a.sessions.now = clock.now
	a.limiter.now = clock.now
	a.accounts.now = clock.now
	return a, clock
}

func codeAt(clock *fakeClock) string {
	return totpCode(rfcSecret, clock.now().Unix()/totpPeriod, totpDigits)
}

func TestLoginCreatesSession(t *testing.T) {
	a, clock := newTestAuth(t)
	id, err := a.Login("Иван", "secret-1", codeAt(clock), "198.51.100.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(id) < 40 {
		t.Fatalf("session id too short: %q", id)
	}
	name, ok := a.Session(id)
	if !ok || name != "Иван" {
		t.Fatalf("session = %q %v", name, ok)
	}
	if _, ok := a.Session(id + "x"); ok {
		t.Fatal("unknown session accepted")
	}
	if _, ok := a.Session(""); ok {
		t.Fatal("empty session accepted")
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	a, clock := newTestAuth(t)
	wrongCode := totpCode(rfcSecret, clock.now().Unix()/totpPeriod+5, totpDigits)
	for _, tc := range []struct{ name, pw, code string }{
		{"Иван", "secret-2", codeAt(clock)},
		{"Иван", "", codeAt(clock)},
		{"Иван", "secret-1", wrongCode},
		{"Иван", "secret-1", ""},
		{"Пётр", "secret-1", codeAt(clock)},
		{"", "", ""},
	} {
		if id, err := a.Login(tc.name, tc.pw, tc.code, "198.51.100.1"); !errors.Is(err, errBadCredentials) || id != "" {
			t.Errorf("%+v: id=%q err=%v", tc, id, err)
		}
	}
}

func TestLoginCodeIsSingleUse(t *testing.T) {
	a, clock := newTestAuth(t)
	code := codeAt(clock)
	if _, err := a.Login("Иван", "secret-1", code, "198.51.100.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login("Иван", "secret-1", code, "198.51.100.1"); !errors.Is(err, errBadCredentials) {
		t.Fatalf("replayed code: %v", err)
	}
	if _, err := a.Login("Олег", "secret-2", code, "198.51.100.1"); err != nil {
		t.Fatalf("other admin's own step rejected: %v", err)
	}
	clock.add(totpPeriod * time.Second)
	if _, err := a.Login("Иван", "secret-1", codeAt(clock), "198.51.100.1"); err != nil {
		t.Fatalf("next step rejected: %v", err)
	}
}

func TestWrongPasswordDoesNotBurnCode(t *testing.T) {
	a, clock := newTestAuth(t)
	code := codeAt(clock)
	if _, err := a.Login("Иван", "nope", code, "198.51.100.1"); !errors.Is(err, errBadCredentials) {
		t.Fatal(err)
	}
	if _, err := a.Login("Иван", "secret-1", code, "198.51.100.1"); err != nil {
		t.Fatalf("code burned by wrong password: %v", err)
	}
}

func TestConcurrentReplayLetsOneIn(t *testing.T) {
	a, clock := newTestAuth(t)
	code := codeAt(clock)
	var ok atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Go(func() {
			<-start
			if _, err := a.Login("Иван", "secret-1", code, "198.51.100.1"); err == nil {
				ok.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d logins with one code", ok.Load())
	}
}

func TestLoginLimiterBlocksIP(t *testing.T) {
	a, clock := newTestAuth(t)
	for i := range loginMaxFailures {
		if _, err := a.Login("Иван", "nope", "000000", "198.51.100.1"); !errors.Is(err, errBadCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := a.Login("Иван", "secret-1", codeAt(clock), "198.51.100.1"); !errors.Is(err, errTooManyAttempts) {
		t.Fatalf("blocked ip got %v", err)
	}
	if _, err := a.Login("Иван", "secret-1", codeAt(clock), "198.51.100.2"); err != nil {
		t.Fatalf("other ip: %v", err)
	}
	clock.add(loginWindow + time.Second)
	if _, err := a.Login("Олег", "secret-2", codeAt(clock), "198.51.100.1"); err != nil {
		t.Fatalf("after window: %v", err)
	}
}

func TestLoginLimiterGroupsIPv6Prefix(t *testing.T) {
	a, clock := newTestAuth(t)
	for i := range loginMaxFailures {
		ip := fmt.Sprintf("2001:db8:1:2::%x", i+1)
		if _, err := a.Login("Иван", "nope", "000000", ip); !errors.Is(err, errBadCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := a.Login("Иван", "secret-1", codeAt(clock), "2001:db8:1:2:ffff::1"); !errors.Is(err, errTooManyAttempts) {
		t.Fatalf("same /64: %v", err)
	}
	if _, err := a.Login("Иван", "secret-1", codeAt(clock), "2001:db8:1:3::1"); err != nil {
		t.Fatalf("other /64: %v", err)
	}
}

func TestSessionExpiresAndLogout(t *testing.T) {
	a, clock := newTestAuth(t)
	id, err := a.Login("Иван", "secret-1", codeAt(clock), "198.51.100.1")
	if err != nil {
		t.Fatal(err)
	}
	clock.add(12*time.Hour - time.Second)
	if _, ok := a.Session(id); !ok {
		t.Fatal("expired too early")
	}
	clock.add(2 * time.Second)
	if _, ok := a.Session(id); ok {
		t.Fatal("session outlived its ttl")
	}
	clock.add(totpPeriod * time.Second)
	id, err = a.Login("Иван", "secret-1", codeAt(clock), "198.51.100.1")
	if err != nil {
		t.Fatal(err)
	}
	a.Logout(id)
	if _, ok := a.Session(id); ok {
		t.Fatal("session survived logout")
	}
}

func TestExpiredSessionsAreSwept(t *testing.T) {
	a, clock := newTestAuth(t)
	if _, err := a.Login("Иван", "secret-1", codeAt(clock), "198.51.100.1"); err != nil {
		t.Fatal(err)
	}
	clock.add(13 * time.Hour)
	if _, err := a.Login("Олег", "secret-2", codeAt(clock), "198.51.100.1"); err != nil {
		t.Fatal(err)
	}
	a.sessions.mu.Lock()
	n := len(a.sessions.m)
	a.sessions.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d sessions kept", n)
	}
}

func TestConcurrentAttemptsStayWithinLimit(t *testing.T) {
	a, _ := newTestAuth(t)
	var bad, blocked atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 60 {
		wg.Go(func() {
			<-start
			_, err := a.Login("Иван", "nope", "000000", "198.51.100.1")
			switch {
			case errors.Is(err, errBadCredentials):
				bad.Add(1)
			case errors.Is(err, errTooManyAttempts):
				blocked.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	if bad.Load() > loginMaxFailures || bad.Load()+blocked.Load() != 60 {
		t.Fatalf("%d checked, %d blocked", bad.Load(), blocked.Load())
	}
}

func TestWrongCodesWithRightPasswordLockAccount(t *testing.T) {
	a, clock := newTestAuth(t)
	wrong := totpCode(rfcSecret, clock.now().Unix()/totpPeriod+7, totpDigits)
	for i := range accountMaxCodeFailures {
		if _, err := a.Login("Иван", "secret-1", wrong, fmt.Sprintf("198.51.100.%d", i+1)); !errors.Is(err, errBadCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := a.Login("Иван", "secret-1", codeAt(clock), "203.0.113.50"); !errors.Is(err, errTooManyAttempts) {
		t.Fatalf("locked account from fresh ip: %v", err)
	}
	if _, err := a.Login("Олег", "secret-2", codeAt(clock), "203.0.113.51"); err != nil {
		t.Fatalf("other admin: %v", err)
	}
	clock.add(loginWindow + time.Second)
	if _, err := a.Login("Иван", "secret-1", codeAt(clock), "203.0.113.52"); err != nil {
		t.Fatalf("after window: %v", err)
	}
}

func TestWrongPasswordsDoNotLockAccount(t *testing.T) {
	a, clock := newTestAuth(t)
	for i := range 3 * loginMaxFailures {
		if _, err := a.Login("Иван", "nope", codeAt(clock), fmt.Sprintf("198.51.100.%d", i+1)); !errors.Is(err, errBadCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := a.Login("Иван", "secret-1", codeAt(clock), "203.0.113.50"); err != nil {
		t.Fatalf("owner locked out by wrong passwords: %v", err)
	}
}

func TestFullLimiterKeepsLoginOpen(t *testing.T) {
	a, _ := newTestAuth(t)
	for i := range maxLimiterKeys {
		a.limiter.attempt(fmt.Sprintf("k%d", i))
	}
	for range loginMaxFailures - 1 {
		a.limiter.attempt("hot")
	}
	for i := range 50 {
		if !a.limiter.attempt(fmt.Sprintf("new%d", i)) {
			t.Fatal("new address refused: a full table must not lock everyone out")
		}
	}
	a.limiter.mu.Lock()
	n := len(a.limiter.m)
	a.limiter.mu.Unlock()
	if n > maxLimiterKeys {
		t.Fatalf("%d keys kept", n)
	}
	if n < maxLimiterKeys*8/10 {
		t.Fatalf("evicted too much: %d left", n)
	}
}

func TestConcurrentCodeGuessesStayWithinAccountLimit(t *testing.T) {
	a, clock := newTestAuth(t)
	cur := clock.now().Unix() / totpPeriod
	var checked, refused atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 40 {
		wg.Go(func() {
			<-start
			code := totpCode(rfcSecret, cur+int64(10+i), totpDigits)
			_, err := a.Login("Иван", "secret-1", code, fmt.Sprintf("198.51.100.%d", i+1))
			switch {
			case errors.Is(err, errCodeAttemptsExhausted) || errors.Is(err, errAccountLocked):
				refused.Add(1)
			case errors.Is(err, errBadCredentials):
				checked.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	if checked.Load() > accountMaxCodeFailures || checked.Load()+refused.Load() != 40 {
		t.Fatalf("%d codes checked, %d refused", checked.Load(), refused.Load())
	}
}

func TestLockedAccountGivesNoPasswordOracle(t *testing.T) {
	a, clock := newTestAuth(t)
	wrong := totpCode(rfcSecret, clock.now().Unix()/totpPeriod+7, totpDigits)
	for i := range accountMaxCodeFailures {
		a.Login("Иван", "secret-1", wrong, fmt.Sprintf("198.51.100.%d", i+1))
	}
	_, right := a.Login("Иван", "secret-1", wrong, "203.0.113.1")
	_, bad := a.Login("Иван", "nope", wrong, "203.0.113.2")
	if !errors.Is(right, errTooManyAttempts) || !errors.Is(bad, errTooManyAttempts) {
		t.Fatalf("locked account answers differ: right=%v wrong=%v", right, bad)
	}
	if errors.Is(errCodeAttemptsExhausted, errTooManyAttempts) || !errors.Is(errCodeAttemptsExhausted, errBadCredentials) {
		t.Fatal("exhausted code slots must look like bad credentials")
	}
}

func TestPasswordChecksAreBounded(t *testing.T) {
	a, _ := newTestAuth(t)
	if cap(a.hashSlots) != maxConcurrentHashes || maxConcurrentHashes < 1 || maxConcurrentHashes > 8 {
		t.Fatalf("hash slots %d", cap(a.hashSlots))
	}
	for range maxConcurrentHashes {
		a.hashSlots <- struct{}{}
	}
	done := make(chan struct{})
	go func() {
		a.Login("Иван", "nope", "000000", "198.51.100.1")
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("password check ran without a free slot")
	case <-time.After(100 * time.Millisecond):
	}
	<-a.hashSlots
	<-done
}
