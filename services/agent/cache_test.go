package agent

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cache.json")
	c := &Cache{Version: 7, Users: []SyncUser{{UUID: "u-1"}, {UUID: "u-2"}}, Inbounds: testInbounds("alpha", "beta")}
	if err := SaveCache(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 7 || len(got.Users) != 2 || got.Users[1].UUID != "u-2" {
		t.Fatalf("got %+v", got)
	}
	if !reflect.DeepEqual(got.Inbounds, c.Inbounds) {
		t.Fatalf("inbounds = %+v", got.Inbounds)
	}
}

func TestCacheKeepsEmptyInboundsApartFromUnknown(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.json")
	if err := SaveCache(empty, &Cache{Version: 1, Inbounds: []Inbound{}}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCache(empty)
	if err != nil {
		t.Fatal(err)
	}
	if got.Inbounds == nil {
		t.Fatal("node without inbounds came back as unknown inbounds")
	}
	unknown := filepath.Join(dir, "unknown.json")
	if err := SaveCache(unknown, &Cache{Version: 1}); err != nil {
		t.Fatal(err)
	}
	if got, err = LoadCache(unknown); err != nil || got.Inbounds != nil {
		t.Fatalf("unknown inbounds came back as %#v (err %v)", got.Inbounds, err)
	}
}

func TestCacheMissingFile(t *testing.T) {
	got, err := LoadCache(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != -1 || got.Users != nil {
		t.Fatalf("absent cache must be version -1, got %+v", got)
	}
}

func TestCacheBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte(`{bad`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCache(path); err == nil {
		t.Fatal("bad json should fail")
	}
}

func TestCacheAtomicOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	first := &Cache{Version: 1, Users: []SyncUser{{UUID: "a"}}}
	if err := SaveCache(path, first); err != nil {
		t.Fatal(err)
	}
	second := &Cache{Version: 2, Users: nil}
	if err := SaveCache(path, second); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.Users != nil {
		t.Fatalf("got %+v", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("tmp file left behind")
	}
}

func TestWriteJSONAtomicSyncsFileAndDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	var synced []string
	orig := fsync
	fsync = func(f *os.File) error {
		synced = append(synced, f.Name())
		return orig(f)
	}
	t.Cleanup(func() { fsync = orig })
	if err := writeJSONAtomic(path, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if len(synced) != 2 || synced[0] != path+".tmp" || synced[1] != dir {
		t.Fatalf("synced = %v, want [%s %s]", synced, path+".tmp", dir)
	}
}

func TestWriteJSONAtomicKeepsOldStateWhenSyncFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := fsync
	fsync = func(f *os.File) error { return errors.New("disk on fire") }
	t.Cleanup(func() { fsync = orig })
	if err := writeJSONAtomic(path, map[string]bool{"new": true}); err == nil {
		t.Fatal("write must fail when data cannot be synced")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"old":true}` {
		t.Fatalf("state replaced by unsynced data: %s", data)
	}
}
