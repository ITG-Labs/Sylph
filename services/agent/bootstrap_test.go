package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapWritesFrameOnlyOnce(t *testing.T) {
	cfg := &Config{XrayAPI: "127.0.0.1:10085", XrayConfig: filepath.Join(t.TempDir(), "xray", "config.json")}
	wrote, err := Bootstrap(cfg)
	if err != nil || !wrote {
		t.Fatalf("wrote = %v, err = %v", wrote, err)
	}
	data, err := os.ReadFile(cfg.XrayConfig)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := BuildXray([]Inbound{}, cfg.XrayAPI)
	if err != nil || !bytes.Equal(data, frame) {
		t.Fatalf("bootstrap wrote %s", data)
	}
	info, err := os.Stat(cfg.XrayConfig)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600: the file will hold REALITY private keys", info.Mode())
	}
	if err := os.WriteFile(cfg.XrayConfig, []byte(`{"applied":"by agent"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	wrote, err = Bootstrap(cfg)
	if err != nil || wrote {
		t.Fatalf("second bootstrap: wrote = %v, err = %v", wrote, err)
	}
	if data, _ := os.ReadFile(cfg.XrayConfig); string(data) != `{"applied":"by agent"}` {
		t.Fatalf("bootstrap overwrote an existing config: %s", data)
	}
}

func TestLoadBootstrapConfig(t *testing.T) {
	unsetEnv(t, "SYLPH_XRAY_API")
	unsetEnv(t, "SYLPH_XRAY_CONFIG")
	unsetEnv(t, "SYLPH_CORE_URL")
	cfg := LoadBootstrapConfig()
	if cfg.XrayAPI != "127.0.0.1:10085" || cfg.XrayConfig != "/etc/xray/config.json" {
		t.Fatalf("cfg = %+v", cfg)
	}
	t.Setenv("SYLPH_XRAY_API", "127.0.0.1:9000")
	t.Setenv("SYLPH_XRAY_CONFIG", "/x/config.json")
	cfg = LoadBootstrapConfig()
	if cfg.XrayAPI != "127.0.0.1:9000" || cfg.XrayConfig != "/x/config.json" {
		t.Fatalf("cfg = %+v", cfg)
	}
}
