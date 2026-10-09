package main

import (
	"crypto/x509"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRevokedList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.json")
	if err := os.WriteFile(path, []byte(`{"serials":["123","456"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	list := loadRevokedList(path)
	if !list.IsRevoked(fakeCert("123")) {
		t.Fatal("serial 123 should be revoked")
	}
	if list.IsRevoked(fakeCert("999")) {
		t.Fatal("serial 999 should not be revoked")
	}
}

func TestLoadRevokedListMissing(t *testing.T) {
	list := loadRevokedList(filepath.Join(t.TempDir(), "missing.json"))
	if list == nil {
		t.Fatal("missing file should give empty list, not nil")
	}
	if list.IsRevoked(fakeCert("123")) {
		t.Fatal("empty list should not revoke anything")
	}
}

func fakeCert(serial string) *x509.Certificate {
	n, ok := new(big.Int).SetString(serial, 10)
	if !ok {
		panic("bad serial")
	}
	return &x509.Certificate{SerialNumber: n}
}
