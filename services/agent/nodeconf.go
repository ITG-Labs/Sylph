package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"
)

const (
	xraySettleUptime = 10 * time.Second
	xrayRestartWait  = 30 * time.Second
	xrayRestartPoll  = 200 * time.Millisecond
)

type rejection struct {
	Hash  string `json:"hash"`
	Error string `json:"error"`
}

type nodeState struct {
	Applied       []Inbound  `json:"applied"`
	XrayRejected  *rejection `json:"xray_rejected,omitempty"`
	CaddyRejected *rejection `json:"caddy_rejected,omitempty"`
}

type NodeConfig struct {
	cfg         *Config
	xray        XrayClient
	caddy       CaddyAPI
	proc        xrayProcess
	logger      *slog.Logger
	buildXray   func([]Inbound) ([]byte, error)
	settle      time.Duration
	restartWait time.Duration
	poll        time.Duration
	state       nodeState
	xrayErr     string
	caddyErr    string
}

func NewNodeConfig(cfg *Config, xray XrayClient, caddy CaddyAPI, logger *slog.Logger) *NodeConfig {
	return &NodeConfig{
		cfg:         cfg,
		xray:        xray,
		caddy:       caddy,
		proc:        procXray{root: "/proc"},
		logger:      logger,
		buildXray:   func(in []Inbound) ([]byte, error) { return BuildXray(in, cfg.XrayAPI) },
		settle:      xraySettleUptime,
		restartWait: xrayRestartWait,
		poll:        xrayRestartPoll,
	}
}

func (n *NodeConfig) Load() error {
	data, err := os.ReadFile(n.cfg.NodeStatePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read node state: %w", err)
	}
	var st nodeState
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("parse node state: %w", err)
	}
	if st.Applied != nil && !n.onDisk(st.Applied) {
		st.Applied = nil
	}
	if st.XrayRejected != nil {
		n.xrayErr = st.XrayRejected.Error
	}
	if st.CaddyRejected != nil {
		n.caddyErr = st.CaddyRejected.Error
	}
	n.state = st
	return nil
}

func (n *NodeConfig) onDisk(inbounds []Inbound) bool {
	want, err := n.buildXray(inbounds)
	if err != nil {
		return false
	}
	path := previousPath(n.cfg.XrayConfig)
	if _, err := os.Stat(path); err != nil {
		path = n.cfg.XrayConfig
	}
	disk, err := os.ReadFile(path)
	return err == nil && bytes.Equal(disk, want)
}

func (n *NodeConfig) RunningTags() []string {
	seen := map[string]bool{}
	var tags []string
	for _, path := range []string{n.cfg.XrayConfig, previousPath(n.cfg.XrayConfig)} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg struct {
			Inbounds []struct {
				Tag string `json:"tag"`
			} `json:"inbounds"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			n.logger.Error("xray_config_unparsable", "path", path, "error", err.Error())
			continue
		}
		for _, in := range cfg.Inbounds {
			if in.Tag != "" && !seen[in.Tag] {
				seen[in.Tag] = true
				tags = append(tags, in.Tag)
			}
		}
	}
	return tags
}

func (n *NodeConfig) Apply(ctx context.Context, desired []Inbound) []Inbound {
	if desired == nil {
		return n.state.Applied
	}
	want, err := n.buildXray(desired)
	if err != nil {
		n.failXray("inbounds_invalid", err)
	} else {
		n.ensureXray(ctx, desired, want)
	}
	if n.state.Applied != nil && ctx.Err() == nil {
		n.ensureCaddy(ctx, n.state.Applied)
	}
	return n.state.Applied
}

func (n *NodeConfig) ConfigError() *string {
	var parts []string
	for _, e := range []string{n.xrayErr, n.caddyErr} {
		if e != "" {
			parts = append(parts, e)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	msg := strings.Join(parts, "; ")
	return &msg
}

func (n *NodeConfig) ensureCaddy(ctx context.Context, applied []Inbound) {
	want, err := BuildCaddy(applied, n.cfg.CaddyAdmin)
	if err != nil {
		n.failCaddy("caddy_config_unbuildable", err)
		return
	}
	sum := configHash(want)
	if r := n.state.CaddyRejected; r != nil && r.Hash == sum {
		n.caddyErr = r.Error
		return
	}
	current, err := n.caddy.Config(ctx)
	if err != nil {
		n.failCaddy("caddy_unreachable", err)
		return
	}
	if sameJSON(current, want) {
		n.acceptCaddy()
		return
	}
	if err := n.caddy.Load(ctx, want); err != nil {
		var rejected *CaddyRejectedError
		if !errors.As(err, &rejected) {
			n.failCaddy("caddy_unreachable", err)
			return
		}
		msg := "caddy_config_rejected: " + truncate(rejected.Message, 300)
		n.state.CaddyRejected = &rejection{Hash: sum, Error: msg}
		n.saveState()
		n.caddyErr = msg
		n.logger.Error("caddy_config_rejected", "config", sum[:12], "status", rejected.Status, "error", truncate(rejected.Message, 300))
		return
	}
	n.acceptCaddy()
	n.logger.Info("caddy_config_applied", "inbounds", len(applied), "config", sum[:12])
}

func (n *NodeConfig) acceptCaddy() {
	n.caddyErr = ""
	if n.state.CaddyRejected != nil {
		n.state.CaddyRejected = nil
		n.saveState()
	}
}

func (n *NodeConfig) failCaddy(code string, err error) {
	n.caddyErr = code + ": " + err.Error()
	n.logger.Error(code, "error", err.Error())
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func (n *NodeConfig) ensureXray(ctx context.Context, desired []Inbound, want []byte) {
	sum := configHash(want)
	if r := n.state.XrayRejected; r != nil && r.Hash == sum {
		n.xrayErr = r.Error
		return
	}
	if err := n.undoInterrupted(); err != nil {
		n.failXray("xray_config_unwritable", err)
		return
	}
	disk, mtime, err := readConfigFile(n.cfg.XrayConfig)
	if err != nil {
		n.failXray("xray_config_unreadable", err)
		return
	}
	uptime, err := n.xray.Uptime(ctx)
	if err != nil {
		n.failXray("xray_unreachable", err)
		return
	}
	if bytes.Equal(disk, want) && mtime.Before(time.Now().Add(-uptime)) {
		n.acceptXray(desired)
		return
	}
	if _, err := n.caddy.Config(ctx); err != nil {
		n.failXray("xray_apply_deferred", fmt.Errorf("caddy admin unreachable, its routes could not follow new xray ports: %w", err))
		return
	}
	if uptime < n.settle {
		n.logger.Info("xray_settling", "xray_uptime_sec", int64(uptime/time.Second))
		if !sleepCtx(ctx, n.settle-uptime) {
			return
		}
	}
	pid, err := n.proc.Find()
	if err != nil {
		n.failXray("xray_process_not_found", err)
		return
	}
	n.restartXray(ctx, desired, want, disk, sum, pid)
}

func (n *NodeConfig) restartXray(ctx context.Context, desired []Inbound, want []byte, disk []byte, sum string, pid int) {
	path := n.cfg.XrayConfig
	prev := disk
	if prev == nil {
		frame, err := n.buildXray([]Inbound{})
		if err != nil {
			n.failXray("xray_config_unwritable", err)
			return
		}
		prev = frame
	}
	if err := writeFileAtomic(previousPath(path), prev); err != nil {
		n.failXray("xray_config_unwritable", err)
		return
	}
	if err := writeFileAtomic(path, want); err != nil {
		_ = os.Remove(previousPath(path))
		n.failXray("xray_config_unwritable", err)
		return
	}
	started := time.Now()
	if err := n.proc.Terminate(pid); err != nil {
		n.restorePrevious()
		n.failXray("xray_signal_failed", err)
		return
	}
	if n.waitRestarted(ctx, started) {
		_ = os.Remove(previousPath(path))
		_ = syncDir(filepath.Dir(path))
		n.acceptXray(desired)
		n.logger.Info("xray_config_applied", "inbounds", len(desired), "config", sum[:12], "restart_ms", time.Since(started).Milliseconds())
		return
	}
	if ctx.Err() != nil {
		return
	}
	n.rollbackXray(ctx, sum)
}

func (n *NodeConfig) rollbackXray(ctx context.Context, sum string) {
	msg := fmt.Sprintf("xray_config_rejected: xray did not come up with the new config within %s", n.restartWait)
	n.restorePrevious()
	started := time.Now()
	if pid, err := n.proc.Find(); err == nil {
		_ = n.proc.Terminate(pid)
	}
	back := n.waitRestarted(ctx, started)
	n.state.XrayRejected = &rejection{Hash: sum, Error: msg}
	n.saveState()
	n.xrayErr = msg
	n.logger.Error("xray_config_rejected", "config", sum[:12], "xray_back", back)
}

func (n *NodeConfig) restorePrevious() {
	path := n.cfg.XrayConfig
	if err := os.Rename(previousPath(path), path); err != nil {
		n.logger.Error("xray_config_restore_failed", "error", err.Error())
		return
	}
	_ = syncDir(filepath.Dir(path))
}

func (n *NodeConfig) undoInterrupted() error {
	path := n.cfg.XrayConfig
	if _, err := os.Stat(previousPath(path)); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Rename(previousPath(path), path); err != nil {
		return err
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		return err
	}
	n.logger.Warn("xray_config_apply_interrupted")
	return syncDir(filepath.Dir(path))
}

func (n *NodeConfig) waitRestarted(ctx context.Context, since time.Time) bool {
	deadline := time.Now().Add(n.restartWait)
	for {
		if up, err := n.xray.Uptime(ctx); err == nil && up < time.Since(since) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !sleepCtx(ctx, n.poll) {
			return false
		}
	}
}

func (n *NodeConfig) acceptXray(desired []Inbound) {
	n.xrayErr = ""
	if reflect.DeepEqual(n.state.Applied, desired) && n.state.XrayRejected == nil {
		return
	}
	n.state.Applied = slices.Clone(desired)
	n.state.XrayRejected = nil
	n.saveState()
}

func (n *NodeConfig) failXray(code string, err error) {
	n.xrayErr = code + ": " + err.Error()
	n.logger.Error(code, "error", err.Error())
}

func (n *NodeConfig) saveState() {
	if err := writeJSONAtomic(n.cfg.NodeStatePath, n.state); err != nil {
		n.logger.Error("node_state_save_failed", "error", err.Error())
	}
}

func readConfigFile(path string) ([]byte, time.Time, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	return data, info.ModTime(), nil
}

func previousPath(path string) string {
	return path + ".previous"
}

func configHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
