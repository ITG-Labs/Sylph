package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
)

type fakeXray struct {
	mu       sync.Mutex
	users    map[string]bool
	stats    map[string]int64
	addErr   error
	inbounds map[string]string
}

func newFakeXray() *fakeXray {
	return &fakeXray{users: map[string]bool{}, stats: map[string]int64{}, inbounds: map[string]string{}}
}

func (f *fakeXray) AddUser(ctx context.Context, tag string, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	f.users[tag+"/"+uuid] = true
	return nil
}

func (f *fakeXray) RemoveUser(ctx context.Context, tag string, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.users, tag+"/"+uuid)
	return nil
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

func (f *fakeXray) AddInbound(ctx context.Context, configJSON string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var cfg struct {
		Tag string `json:"tag"`
	}
	if err := jsonUnmarshalString(configJSON, &cfg); err != nil {
		return errors.New("bad json")
	}
	if cfg.Tag == "" {
		return errors.New("no tag")
	}
	f.inbounds[cfg.Tag] = configJSON
	return nil
}

func (f *fakeXray) RemoveInbound(ctx context.Context, tag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.inbounds, tag)
	return nil
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
