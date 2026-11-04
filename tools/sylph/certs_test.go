package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/castore"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/bundle"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/role"
	"github.com/ITG-Labs/sylph/tools/sylph/internal/ui"
)

func newCA(t *testing.T, issued time.Time) string {
	t.Helper()
	ca := t.TempDir()
	if err := castore.Init(ca, "CA", issued); err != nil {
		t.Fatal(err)
	}
	return ca
}

func runCerts(t *testing.T, args ...string) error {
	t.Helper()
	return cmdCerts(context.Background(), ui.New(), args)
}

func TestBundleRefusesRevokedLeafAndPointsToRenew(t *testing.T) {
	now := time.Now()
	ca := newCA(t, now)
	if err := castore.Issue(ca, "node-1", mtls.ClientUsage, mtls.RoleNode, certTTL, now); err != nil {
		t.Fatal(err)
	}
	if err := castore.Revoke(ca, "node-1"); err != nil {
		t.Fatal(err)
	}
	err := runCerts(t, "bundle", "node", "--name", "node-1", "--ca", ca, "--file", "--out", t.TempDir())
	if err == nil {
		t.Fatal("bundle must refuse a revoked leaf")
	}
	if !strings.Contains(err.Error(), "отозван") || !strings.Contains(err.Error(), "certs renew node --name node-1") {
		t.Fatalf("error = %v, want revoked hint with renew command", err)
	}
}

func TestBundleRefusesExpiredLeafAndPointsToRenew(t *testing.T) {
	past := time.Now().Add(-2 * certTTL)
	ca := newCA(t, past)
	if err := castore.Issue(ca, "bot", mtls.ClientUsage, mtls.RoleBot, certTTL, past); err != nil {
		t.Fatal(err)
	}
	err := runCerts(t, "bundle", "bot", "--ca", ca, "--file", "--out", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "истёк") || !strings.Contains(err.Error(), "certs renew bot") {
		t.Fatalf("error = %v, want expired hint with renew command", err)
	}
}

func TestCheckLeafWarnsAboutExpiringCert(t *testing.T) {
	now := time.Now()
	ca := newCA(t, now)
	if err := castore.Issue(ca, "bot", mtls.ClientUsage, mtls.RoleBot, 10*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	warn, err := checkLeaf(ca, "bot", "certs renew bot", now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warn, "certs renew bot") {
		t.Fatalf("warning = %q, want renew hint", warn)
	}
	if err := castore.Issue(ca, "subs", mtls.ClientUsage, mtls.RoleSubs, certTTL, now); err != nil {
		t.Fatal(err)
	}
	if warn, err := checkLeaf(ca, "subs", "certs renew subs", now); err != nil || warn != "" {
		t.Fatalf("healthy leaf = %q, %v", warn, err)
	}
}

func TestRenewAfterRevokeRestoresNodeBundle(t *testing.T) {
	now := time.Now()
	ca := newCA(t, now)
	if err := castore.Issue(ca, "node-1", mtls.ClientUsage, mtls.RoleNode, certTTL, now); err != nil {
		t.Fatal(err)
	}
	if err := castore.Revoke(ca, "node-1"); err != nil {
		t.Fatal(err)
	}
	if err := runCerts(t, "renew", "node", "--name", "node-1", "--ca", ca); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := runCerts(t, "bundle", "node", "--name", "node-1", "--ca", ca, "--file", "--out", out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "node-1"+bundle.Ext))
	if err != nil {
		t.Fatal(err)
	}
	b, err := bundle.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Verify("node", role.DefaultCoreName, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestRenewCoreReissuesServerLeafAndCron(t *testing.T) {
	now := time.Now()
	ca := newCA(t, now)
	if err := runCerts(t, "bundle", "core", "--ca", ca, "--file", "--out", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	before, err := castore.List(ca)
	if err != nil || len(before) != 2 {
		t.Fatalf("list = %v, %v", before, err)
	}
	if err := castore.Revoke(ca, "cron"); err != nil {
		t.Fatal(err)
	}
	if err := runCerts(t, "bundle", "core", "--ca", ca, "--file", "--out", t.TempDir()); err == nil {
		t.Fatal("bundle core must refuse the revoked cron leaf")
	}
	if err := runCerts(t, "renew", "core", "--ca", ca); err != nil {
		t.Fatal(err)
	}
	after, err := castore.List(ca)
	if err != nil || len(after) != 2 {
		t.Fatalf("list = %v, %v", after, err)
	}
	for i := range before {
		if before[i].Name != after[i].Name || before[i].Serial == after[i].Serial {
			t.Fatalf("leaf %s was not reissued: %v -> %v", before[i].Name, before[i].Serial, after[i].Serial)
		}
	}
	out := t.TempDir()
	if err := runCerts(t, "bundle", "core", "--ca", ca, "--file", "--out", out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "core"+bundle.Ext))
	if err != nil {
		t.Fatal(err)
	}
	b, err := bundle.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Files[bundle.RevokedFile]; !ok {
		t.Fatal("core bundle must carry revoked.json")
	}
	if _, err := b.Verify("core", role.DefaultCoreName, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestRenewRequiresExistingLeaf(t *testing.T) {
	ca := newCA(t, time.Now())
	if err := runCerts(t, "renew", "bot", "--ca", ca); err == nil || !strings.Contains(err.Error(), "certs bundle bot") {
		t.Fatalf("error = %v, want hint to bundle first", err)
	}
	if err := runCerts(t, "renew", "node", "--ca", ca); err == nil {
		t.Fatal("node renew without --name must fail")
	}
	if err := runCerts(t, "renew", "bot", "--name", "x", "--ca", ca); err == nil {
		t.Fatal("--name is node-only")
	}
}
