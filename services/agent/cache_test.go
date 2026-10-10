package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cache.json")
	c := &Cache{Version: 7, Users: []SyncUser{{UUID: "u-1"}, {UUID: "u-2"}}, Config: "{}"}
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
