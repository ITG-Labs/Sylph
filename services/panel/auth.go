package panel

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/netip"
	"sync"
	"time"
)

const (
	loginMaxFailures = 10
	loginWindow      = 15 * time.Minute
	maxLimiterKeys   = 10000
)

var (
	errBadCredentials  = errors.New("bad credentials")
	errTooManyAttempts = errors.New("too many attempts")
)

type Auth struct {
	admins   map[string]*admin
	dummy    *passwordHash
	sessions *sessions
	limiter  *loginLimiter
	now      func() time.Time

	mu       sync.Mutex
	lastStep map[string]int64
}

func NewAuth(admins map[string]*admin, sessionTTL time.Duration) *Auth {
	iter := 1
	for _, a := range admins {
		iter = max(iter, a.password.iter)
	}
	return &Auth{
		admins:   admins,
		dummy:    &passwordHash{iter: iter, salt: make([]byte, 16), key: make([]byte, passwordKeyLen)},
		sessions: &sessions{ttl: sessionTTL, m: map[string]session{}, now: time.Now},
		limiter:  &loginLimiter{m: map[string]*attempts{}, now: time.Now},
		now:      time.Now,
		lastStep: map[string]int64{},
	}
}

func (a *Auth) Login(name, password, code, ip string) (string, error) {
	key := limiterKey(ip)
	if a.limiter.blocked(key) {
		return "", errTooManyAttempts
	}
	adm := a.admins[name]
	if adm == nil {
		a.dummy.verify(password)
		a.limiter.fail(key)
		return "", errBadCredentials
	}
	if !adm.password.verify(password) || !a.consumeCode(adm, code) {
		a.limiter.fail(key)
		return "", errBadCredentials
	}
	a.limiter.reset(key)
	return a.sessions.create(adm.name)
}

func (a *Auth) consumeCode(adm *admin, code string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	step, ok := verifyTOTP(adm.secret, code, a.now(), a.lastStep[adm.name])
	if ok {
		a.lastStep[adm.name] = step
	}
	return ok
}

func (a *Auth) Session(id string) (string, bool) {
	name, ok := a.sessions.lookup(id)
	if !ok || a.admins[name] == nil {
		return "", false
	}
	return name, true
}

func (a *Auth) Logout(id string) {
	a.sessions.remove(id)
}

type session struct {
	name    string
	expires time.Time
}

type sessions struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]session
	now func() time.Time
}

func (s *sessions) create(name string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.m {
		if !now.Before(v.expires) {
			delete(s.m, k)
		}
	}
	s.m[id] = session{name: name, expires: now.Add(s.ttl)}
	return id, nil
}

func (s *sessions) lookup(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	if !ok {
		return "", false
	}
	if !s.now().Before(v.expires) {
		delete(s.m, id)
		return "", false
	}
	return v.name, true
}

func (s *sessions) remove(id string) {
	s.mu.Lock()
	delete(s.m, id)
	s.mu.Unlock()
}

type attempts struct {
	first time.Time
	n     int
}

type loginLimiter struct {
	mu  sync.Mutex
	m   map[string]*attempts
	now func() time.Time
}

func (l *loginLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	at := l.m[key]
	return at != nil && l.now().Sub(at.first) <= loginWindow && at.n >= loginMaxFailures
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.m) >= maxLimiterKeys {
		for k, v := range l.m {
			if now.Sub(v.first) > loginWindow {
				delete(l.m, k)
			}
		}
	}
	at := l.m[key]
	if at == nil || now.Sub(at.first) > loginWindow {
		l.m[key] = &attempts{first: now, n: 1}
		return
	}
	at.n++
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.m, key)
	l.mu.Unlock()
}

func limiterKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is6() {
		p, _ := addr.Prefix(64)
		return p.String()
	}
	return addr.String()
}
