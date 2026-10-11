package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type fakeXray struct {
	mu         sync.Mutex
	users      map[string]bool
	stats      map[string]int64
	addErr     error
	failAdd    map[string]bool
	failRemove map[string]bool
	listErr    error
	uptime     time.Duration
	uptimeErr  error
	uptimeFail int
	uptimeHits int
}

func newFakeXray() *fakeXray {
	return &fakeXray{users: map[string]bool{}, stats: map[string]int64{}}
}

func (f *fakeXray) AddUser(ctx context.Context, tag string, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	if f.failAdd[uuid] {
		return errors.New("add failed for " + uuid)
	}
	f.users[tag+"/"+uuid] = true
	return nil
}

func (f *fakeXray) RemoveUser(ctx context.Context, tag string, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRemove[uuid] {
		return errors.New("remove failed for " + uuid)
	}
	delete(f.users, tag+"/"+uuid)
	return nil
}

func (f *fakeXray) ListUsers(ctx context.Context, tag string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []string
	for k := range f.users {
		if strings.HasPrefix(k, tag+"/") {
			out = append(out, strings.TrimPrefix(k, tag+"/"))
		}
	}
	return out, nil
}

func (f *fakeXray) wipe() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users = map[string]bool{}
	f.stats = map[string]int64{}
}

func (f *fakeXray) set(field func(*fakeXray)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	field(f)
}

func (f *fakeXray) QueryStats(ctx context.Context, pattern string) (map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int64{}
	for k, v := range f.stats {
		if strings.HasPrefix(k, pattern) {
			out[k] = v
		}
	}
	return out, nil
}

func (f *fakeXray) setStats(stats map[string]int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats = stats
}

func (f *fakeXray) hasUser(tag, uuid string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[tag+"/"+uuid]
}

func (f *fakeXray) userCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.users)
}

func (f *fakeXray) Uptime(ctx context.Context) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uptimeHits++
	if f.uptimeFail > 0 {
		f.uptimeFail--
		return 0, errors.New("xray is starting")
	}
	if f.uptimeErr != nil {
		return 0, f.uptimeErr
	}
	return f.uptime, nil
}

func (f *fakeXray) uptimeCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uptimeHits
}
