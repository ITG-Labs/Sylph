package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/castore"
	"github.com/ITG-Labs/sylph/pkg/mtls"
)

var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func newCA(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := castore.Init(dir, "Sylph CA", now); err != nil {
		t.Fatal(err)
	}
	return dir
}

func issue(t *testing.T, dir string, name string, usage mtls.LeafUsage, role string) {
	t.Helper()
	if err := castore.Issue(dir, name, usage, role, 365*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
}

func roundTrip(t *testing.T, b *Bundle) *Bundle {
	t.Helper()
	data, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return back
}

func TestCoreBundle(t *testing.T) {
	dir := newCA(t)
	issue(t, dir, "core.sylph.internal", mtls.ServerUsage, "")
	issue(t, dir, "cron", mtls.ClientUsage, mtls.RoleCron)
	issue(t, dir, "node-1", mtls.ClientUsage, mtls.RoleNode)
	if err := castore.Revoke(dir, "node-1"); err != nil {
		t.Fatal(err)
	}
	b, err := Collect(dir, "core", "core", "core.sylph.internal", now)
	if err != nil {
		t.Fatal(err)
	}
	back := roundTrip(t, b)
	if _, ok := back.Files[RevokedFile]; !ok {
		t.Fatal("revoked.json should travel with the core bundle")
	}
	leaves, err := back.Verify("core", "core.sylph.internal", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaves) != 2 || leaves[0].Name != "core.sylph.internal" || leaves[1].Role != mtls.RoleCron {
		t.Fatalf("leaves = %+v", leaves)
	}
	if _, err := back.Verify("core", "other.name", now); err == nil || !strings.Contains(err.Error(), "SYLPH_CORE_NAME") {
		t.Fatalf("wrong core name: %v", err)
	}
	if _, err := back.Verify("panel", "core.sylph.internal", now); err == nil {
		t.Fatal("core bundle must not install as panel")
	}
}

func TestNodeBundleInline(t *testing.T) {
	dir := newCA(t)
	issue(t, dir, "node-de-1", mtls.ClientUsage, mtls.RoleNode)
	b, err := Collect(dir, "node", "node-de-1", "", now)
	if err != nil {
		t.Fatal(err)
	}
	line, err := b.Inline()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, Prefix) || strings.ContainsAny(line, "\n ") {
		t.Fatalf("inline form = %q", line)
	}
	wrapped := line[:40] + "\n  " + line[40:]
	back, err := Load(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	leaves, err := back.Verify("node", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if leaves[0].Name != "node-de-1" || leaves[0].Role != mtls.RoleNode {
		t.Fatalf("leaf = %+v", leaves[0])
	}
	path := filepath.Join(t.TempDir(), "node-de-1"+Ext)
	data, _ := b.Encode()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("load from file: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	dir := newCA(t)
	issue(t, dir, "panel", mtls.ClientUsage, mtls.RolePanel)
	issue(t, dir, "bot", mtls.ClientUsage, mtls.RoleBot)
	other := newCA(t)
	good, err := Collect(dir, "panel", "panel", "", now)
	if err != nil {
		t.Fatal(err)
	}
	botBundle, _ := Collect(dir, "bot", "bot", "", now)
	foreign, _ := os.ReadFile(castore.CACertPath(other))

	cases := map[string]struct {
		mutate func(b *Bundle)
		at     time.Time
		want   string
	}{
		"foreign CA":       {func(b *Bundle) { b.Files[CAFile] = foreign }, now, "не подписан"},
		"key from another": {func(b *Bundle) { b.Files["key.pem"] = botBundle.Files["key.pem"] }, now, "не от серта"},
		"cert of bot role": {func(b *Bundle) { b.Files["cert.pem"] = botBundle.Files["cert.pem"]; b.Files["key.pem"] = botBundle.Files["key.pem"] }, now, "с ролью \"bot\""},
		"expired":          {func(b *Bundle) {}, now.Add(400 * 24 * time.Hour), "истёк"},
		"missing key":      {func(b *Bundle) { delete(b.Files, "key.pem") }, now, "нет key.pem"},
		"leaf as CA":       {func(b *Bundle) { b.Files[CAFile] = b.Files["cert.pem"] }, now, "не серт CA"},
	}
	for name, c := range cases {
		b := roundTrip(t, good)
		c.mutate(b)
		if _, err := b.Verify("panel", "", c.at); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}

func TestVerifyRejectsOwnRevokedLeaf(t *testing.T) {
	dir := newCA(t)
	issue(t, dir, "core.sylph.internal", mtls.ServerUsage, "")
	issue(t, dir, "cron", mtls.ClientUsage, mtls.RoleCron)
	if err := castore.Revoke(dir, "cron"); err != nil {
		t.Fatal(err)
	}
	b, _ := Collect(dir, "core", "core", "core.sylph.internal", now)
	if _, err := roundTrip(t, b).Verify("core", "core.sylph.internal", now); err == nil || !strings.Contains(err.Error(), "отозван") {
		t.Fatalf("err = %v", err)
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestDecodeRejects(t *testing.T) {
	manifest := `{"version":1,"role":"panel","name":"panel"}`
	cases := map[string][]byte{
		"not gzip":     []byte("hello"),
		"no manifest":  tarGz(t, map[string]string{"ca.pem": "x"}),
		"traversal":    tarGz(t, map[string]string{manifestName: manifest, "../../etc/passwd": "x"}),
		"foreign file": tarGz(t, map[string]string{manifestName: manifest, "revoked.json": "{}"}),
		"new format":   tarGz(t, map[string]string{manifestName: `{"version":2,"role":"panel"}`}),
	}
	for name, data := range cases {
		if _, err := Decode(data); err == nil {
			t.Errorf("%s: should fail", name)
		}
	}
	if _, err := Load("sylph-certs:!!!"); err == nil {
		t.Error("bad base64 should fail")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "нет файла") {
		t.Errorf("missing file: %v", err)
	}
}

func TestReadFromStream(t *testing.T) {
	dir := newCA(t)
	issue(t, dir, "subs", mtls.ClientUsage, mtls.RoleSubs)
	b, _ := Collect(dir, "subs", "subs", "", now)
	raw, _ := b.Encode()
	line, _ := b.Inline()
	for name, in := range map[string][]byte{"gzip": raw, "inline": []byte(line + "\n")} {
		got, err := Read(bytes.NewReader(in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := got.Verify("subs", "", now); err != nil {
			t.Fatalf("%s verify: %v", name, err)
		}
	}
}
