package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Cache struct {
	Version  int64      `json:"version"`
	Users    []SyncUser `json:"users"`
	Inbounds []Inbound  `json:"inbounds"`
}

func LoadCache(path string) (*Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Cache{Version: -1}, nil
		}
		return nil, fmt.Errorf("read cache: %w", err)
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse cache: %w", err)
	}
	return &c, nil
}

func SaveCache(path string, c *Cache) error {
	return writeJSONAtomic(path, c)
}

var fsync = func(f *os.File) error { return f.Sync() }

func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return writeFileAtomic(path, data)
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := writeSynced(tmp, data); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return syncDir(dir)
}

func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := fsync(f); err != nil {
		f.Close()
		return fmt.Errorf("sync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open state dir: %w", err)
	}
	defer d.Close()
	if err := fsync(d); err != nil {
		return fmt.Errorf("sync state dir: %w", err)
	}
	return nil
}
