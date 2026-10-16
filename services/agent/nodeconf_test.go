package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const brokenKey = "broken-key-xray-refuses-it"

type fakeProc struct {
	mu       sync.Mutex
	fx       *fakeXray
	path     string
	findErr  error
	termErr  error
	dieAfter time.Duration
	kills    int
	killedAt time.Time
}

func (p *fakeProc) Find() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.findErr != nil {
		return 0, p.findErr
	}
	return 42, nil
}

func (p *fakeProc) Terminate(pid int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.termErr != nil {
		return p.termErr
	}
	p.kills++
	p.killedAt = time.Now()
	data, _ := os.ReadFile(p.path)
	restart := func() {
		p.fx.set(func(f *fakeXray) {
			f.users = map[string]bool{}
			f.rules = staticRules(data)
			f.uptime = 0
			f.uptimeErr = nil
			if bytes.Contains(data, []byte(brokenKey)) {
				f.uptimeErr = errors.New("xray is down")
			}
		})
	}
	if p.dieAfter > 0 {
		time.AfterFunc(p.dieAfter, restart)
		return nil
	}
	restart()
	return nil
}

func (p *fakeProc) killCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kills
}

type fakeCaddy struct {
	mu       sync.Mutex
	current  []byte
	reject   string
	dropLoad bool
	loads    int
	loadedAt time.Time
}

func (f *fakeCaddy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/config/":
		if f.current == nil {
			_, _ = w.Write([]byte("null\n"))
			return
		}
		_, _ = w.Write(f.current)
	case r.Method == http.MethodPost && r.URL.Path == "/load":
		body, _ := io.ReadAll(r.Body)
		if f.dropLoad {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		if f.reject != "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": f.reject})
			return
		}
		f.current = body
		f.loads++
		f.loadedAt = time.Now()
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeCaddy) set(field func(*fakeCaddy)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	field(f)
}

func (f *fakeCaddy) state() ([]byte, int, time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current, f.loads, f.loadedAt
}

type nodeRig struct {
	t     *testing.T
	cfg   *Config
	fx    *fakeXray
	proc  *fakeProc
	caddy *fakeCaddy
	node  *NodeConfig
	logs  *bytes.Buffer
}

func newNodeRig(t *testing.T, fx *fakeXray) *nodeRig {
	t.Helper()
	dir := t.TempDir()
	fc := &fakeCaddy{}
	srv := httptest.NewServer(fc)
	t.Cleanup(srv.Close)
	cfg := &Config{
		XrayAPI:       "127.0.0.1:10085",
		XrayConfig:    filepath.Join(dir, "xray", "config.json"),
		NodeStatePath: filepath.Join(dir, "state", "node.json"),
		CaddyAdmin:    strings.TrimPrefix(srv.URL, "http://"),
	}
	r := &nodeRig{t: t, cfg: cfg, fx: fx, caddy: fc, logs: &bytes.Buffer{}}
	r.proc = &fakeProc{fx: fx, path: cfg.XrayConfig}
	r.node = r.newNode()
	if _, err := Bootstrap(cfg); err != nil {
		t.Fatal(err)
	}
	r.age(time.Hour)
	fx.set(func(f *fakeXray) { f.uptime = 30 * time.Minute })
	return r
}

func (r *nodeRig) newNode() *NodeConfig {
	n := NewNodeConfig(r.cfg, r.fx, NewCaddyAPI(r.cfg.CaddyAdmin, 2*time.Second), slog.New(slog.NewJSONHandler(r.logs, nil)))
	n.proc = r.proc
	n.settle = 0
	n.restartWait = 100 * time.Millisecond
	n.poll = 5 * time.Millisecond
	return n
}

func (r *nodeRig) age(d time.Duration) {
	r.t.Helper()
	old := time.Now().Add(-d)
	if err := os.Chtimes(r.cfg.XrayConfig, old, old); err != nil {
		r.t.Fatal(err)
	}
}

func (r *nodeRig) disk() []byte {
	r.t.Helper()
	data, err := os.ReadFile(r.cfg.XrayConfig)
	if err != nil {
		r.t.Fatal(err)
	}
	return data
}

func (r *nodeRig) built(in []Inbound) []byte {
	r.t.Helper()
	data, err := BuildXray(in, r.cfg.XrayAPI)
	if err != nil {
		r.t.Fatal(err)
	}
	return data
}

func (r *nodeRig) noPrevious() {
	r.t.Helper()
	if _, err := os.Stat(previousPath(r.cfg.XrayConfig)); !errors.Is(err, os.ErrNotExist) {
		r.t.Fatalf("previous config left on disk: %v", err)
	}
}

func brokenInbounds() []Inbound {
	in := testInbound("next")
	in.Reality.PrivateKey = brokenKey
	return []Inbound{in}
}

func TestApplyWritesConfigAndRestartsXray(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	got := r.node.Apply(context.Background(), mainIn)
	if !reflect.DeepEqual(got, mainIn) {
		t.Fatalf("effective inbounds = %+v", got)
	}
	if r.proc.killCount() != 1 || !bytes.Equal(r.disk(), r.built(mainIn)) {
		t.Fatalf("kills = %d, disk = %s", r.proc.killCount(), r.disk())
	}
	if e := r.node.ConfigError(); e != nil {
		t.Fatalf("config_error = %q", *e)
	}
	r.noPrevious()
	var st nodeState
	raw, err := os.ReadFile(r.cfg.NodeStatePath)
	if err != nil || json.Unmarshal(raw, &st) != nil || !reflect.DeepEqual(st.Applied, mainIn) {
		t.Fatalf("node state = %s (%v)", raw, err)
	}
}

func TestApplySameConfigAlreadyRunningDoesNotTouchXray(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	if err := writeFileAtomic(r.cfg.XrayConfig, r.built(mainIn)); err != nil {
		t.Fatal(err)
	}
	r.age(time.Hour)
	got := r.node.Apply(context.Background(), mainIn)
	if r.proc.killCount() != 0 {
		t.Fatal("xray restarted although it already runs this config")
	}
	if !reflect.DeepEqual(got, mainIn) {
		t.Fatalf("effective inbounds = %+v, want the running ones", got)
	}
	again := r.newNode()
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	again.Apply(context.Background(), mainIn)
	if r.proc.killCount() != 0 {
		t.Fatal("agent restart restarted xray")
	}
}

func TestApplyRestartsXrayThatHasNotReadTheFile(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	if err := writeFileAtomic(r.cfg.XrayConfig, r.built(mainIn)); err != nil {
		t.Fatal(err)
	}
	r.node.Apply(context.Background(), mainIn)
	if r.proc.killCount() != 1 {
		t.Fatalf("kills = %d: file written after xray started must trigger a restart", r.proc.killCount())
	}
}

func TestApplyUnknownInboundsTouchesNothing(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	before := r.disk()
	if got := r.node.Apply(context.Background(), nil); got != nil {
		t.Fatalf("effective inbounds = %+v, want unknown", got)
	}
	if r.proc.killCount() != 0 || !bytes.Equal(r.disk(), before) {
		t.Fatal("unknown inbounds touched xray")
	}
	if _, err := os.Stat(r.cfg.NodeStatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unknown inbounds wrote node state")
	}
}

func TestApplyWaitsForTheNewXrayProcess(t *testing.T) {
	fx := newFakeXray()
	r := newNodeRig(t, fx)
	r.proc.dieAfter = 50 * time.Millisecond
	r.node.restartWait = 2 * time.Second
	r.node.Apply(context.Background(), mainIn)
	up, err := fx.Uptime(context.Background())
	if err != nil || up != 0 {
		t.Fatalf("Apply returned while the old xray was still running (uptime %v, err %v): users would go to a dying process", up, err)
	}
}

func TestApplyRollsBackConfigXrayRejects(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.node.Apply(ctx, mainIn)
	got := r.node.Apply(ctx, brokenInbounds())
	if !reflect.DeepEqual(got, mainIn) {
		t.Fatalf("effective inbounds after rollback = %+v, want the old ones", got)
	}
	if !bytes.Equal(r.disk(), r.built(mainIn)) {
		t.Fatalf("disk after rollback = %s", r.disk())
	}
	r.noPrevious()
	if r.proc.killCount() != 3 {
		t.Fatalf("kills = %d, want apply, try new, roll back", r.proc.killCount())
	}
	e := r.node.ConfigError()
	if e == nil || !strings.HasPrefix(*e, "xray_config_rejected: ") {
		t.Fatalf("config_error = %v", e)
	}
	if _, err := r.fx.Uptime(ctx); err != nil {
		t.Fatalf("xray left down after rollback: %v", err)
	}
	if !strings.Contains(r.logs.String(), `"msg":"xray_config_rejected"`) {
		t.Fatalf("no rejection log: %s", r.logs.String())
	}
}

func TestRejectedConfigIsNotRetriedUntilInboundsChange(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.node.Apply(ctx, mainIn)
	r.node.Apply(ctx, brokenInbounds())
	kills := r.proc.killCount()
	start := time.Now()
	r.node.Apply(ctx, brokenInbounds())
	if r.proc.killCount() != kills || time.Since(start) > 50*time.Millisecond {
		t.Fatal("rejected config applied again")
	}
	if e := r.node.ConfigError(); e == nil || !strings.HasPrefix(*e, "xray_config_rejected: ") {
		t.Fatalf("config_error lost: %v", e)
	}
	restarted := r.newNode()
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	if e := restarted.ConfigError(); e == nil || !strings.HasPrefix(*e, "xray_config_rejected: ") {
		t.Fatalf("config_error lost across agent restart: %v", e)
	}
	if got := restarted.Apply(ctx, brokenInbounds()); !reflect.DeepEqual(got, mainIn) || r.proc.killCount() != kills {
		t.Fatalf("restarted agent retried the rejected config: kills %d, effective %+v", r.proc.killCount(), got)
	}
	fixed := testInbounds("main", "second")
	if got := restarted.Apply(ctx, fixed); !reflect.DeepEqual(got, fixed) {
		t.Fatalf("new inbounds not applied after a rejection: %+v", got)
	}
	if restarted.ConfigError() != nil {
		t.Fatalf("config_error = %q after a good apply", *restarted.ConfigError())
	}
}

func TestBackToRunningInboundsClearsRejection(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.node.Apply(ctx, mainIn)
	r.node.Apply(ctx, brokenInbounds())
	r.fx.set(func(f *fakeXray) { f.uptime = time.Hour })
	r.age(2 * time.Hour)
	kills := r.proc.killCount()
	r.node.Apply(ctx, mainIn)
	if r.node.ConfigError() != nil || r.proc.killCount() != kills {
		t.Fatalf("reverting inbounds in core must clear the error without a restart: %v, kills %d", r.node.ConfigError(), r.proc.killCount())
	}
}

func TestApplyWaitsUntilXraySettled(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.node.settle = 300 * time.Millisecond
	r.fx.set(func(f *fakeXray) { f.uptime = 0 })
	r.age(time.Hour)
	start := time.Now()
	r.node.Apply(context.Background(), mainIn)
	r.proc.mu.Lock()
	waited := r.proc.killedAt.Sub(start)
	r.proc.mu.Unlock()
	if r.proc.killCount() != 1 || waited < 250*time.Millisecond {
		t.Fatalf("killed after %v, want a wait until xray has been up for the settle time", waited)
	}
}

func TestApplySettleWaitStopsOnShutdown(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.node.settle = time.Hour
	r.fx.set(func(f *fakeXray) { f.uptime = 0 })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	done := make(chan struct{})
	go func() {
		r.node.Apply(ctx, mainIn)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGTERM did not interrupt the settle wait")
	}
	if r.proc.killCount() != 0 {
		t.Fatal("xray restarted during shutdown")
	}
}

func TestApplyWithoutXrayProcessTouchesNothing(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.proc.findErr = errXrayNotFound
	before := r.disk()
	r.node.Apply(context.Background(), mainIn)
	if !bytes.Equal(r.disk(), before) {
		t.Fatal("config written although xray cannot be restarted")
	}
	r.noPrevious()
	if e := r.node.ConfigError(); e == nil || !strings.HasPrefix(*e, "xray_process_not_found: ") {
		t.Fatalf("config_error = %v", e)
	}
	r.proc.findErr = nil
	r.node.Apply(context.Background(), mainIn)
	if r.proc.killCount() != 1 || r.node.ConfigError() != nil {
		t.Fatal("not a rejection: must apply once xray is reachable")
	}
}

func TestApplyXrayUnreachableDefers(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.fx.set(func(f *fakeXray) { f.uptimeErr = errors.New("connection refused") })
	before := r.disk()
	r.node.Apply(context.Background(), mainIn)
	if !bytes.Equal(r.disk(), before) || r.proc.killCount() != 0 {
		t.Fatal("touched xray that does not answer")
	}
	if e := r.node.ConfigError(); e == nil || !strings.HasPrefix(*e, "xray_unreachable: ") {
		t.Fatalf("config_error = %v", e)
	}
	if r.node.state.XrayRejected != nil {
		t.Fatal("unreachable xray recorded as a rejection")
	}
}

func TestApplySignalFailureKeepsOldConfig(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	r.proc.termErr = errors.New("operation not permitted")
	before := r.disk()
	r.node.Apply(context.Background(), mainIn)
	if !bytes.Equal(r.disk(), before) {
		t.Fatalf("disk = %s, want the old config back", r.disk())
	}
	r.noPrevious()
	if e := r.node.ConfigError(); e == nil || !strings.HasPrefix(*e, "xray_signal_failed: ") {
		t.Fatalf("config_error = %v", e)
	}
	if r.node.state.XrayRejected != nil {
		t.Fatal("failed signal recorded as a rejection")
	}
}

func TestApplyUndoesInterruptedApply(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	frame := r.disk()
	if err := writeFileAtomic(previousPath(r.cfg.XrayConfig), frame); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(r.cfg.XrayConfig, r.built(brokenInbounds())); err != nil {
		t.Fatal(err)
	}
	r.fx.set(func(f *fakeXray) { f.uptimeErr = errors.New("xray is down") })
	n := r.newNode()
	if err := n.Load(); err != nil {
		t.Fatal(err)
	}
	n.Apply(context.Background(), mainIn)
	if !bytes.Equal(r.disk(), frame) {
		t.Fatalf("interrupted apply not undone: disk = %s", r.disk())
	}
	r.noPrevious()
	r.fx.set(func(f *fakeXray) { f.uptimeErr = nil; f.uptime = time.Hour })
	if got := n.Apply(context.Background(), mainIn); !reflect.DeepEqual(got, mainIn) || !bytes.Equal(r.disk(), r.built(mainIn)) {
		t.Fatalf("apply after undo failed: %+v", got)
	}
}

func TestApplyUndoForcesRestartEvenForSameContent(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	want := r.built(mainIn)
	if err := writeFileAtomic(previousPath(r.cfg.XrayConfig), want); err != nil {
		t.Fatal(err)
	}
	r.node.Apply(context.Background(), mainIn)
	if r.proc.killCount() != 1 {
		t.Fatal("after an interrupted apply xray may run anything: must restart")
	}
}

func TestApplyWithoutConfigFileRollsBackToFrame(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	if err := os.Remove(r.cfg.XrayConfig); err != nil {
		t.Fatal(err)
	}
	r.node.Apply(context.Background(), brokenInbounds())
	if !bytes.Equal(r.disk(), r.built([]Inbound{})) {
		t.Fatalf("rollback without a previous config must leave the frame, got %s", r.disk())
	}
}

func TestInvalidInboundsKeepRunningConfig(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.node.Apply(ctx, mainIn)
	bad := testInbound("main")
	bad.Transport = "xhttp"
	got := r.node.Apply(ctx, []Inbound{bad})
	if !reflect.DeepEqual(got, mainIn) || r.proc.killCount() != 1 || !bytes.Equal(r.disk(), r.built(mainIn)) {
		t.Fatalf("invalid inbounds touched xray: effective %+v, kills %d", got, r.proc.killCount())
	}
	if e := r.node.ConfigError(); e == nil || !strings.HasPrefix(*e, "inbounds_invalid: ") || !strings.Contains(*e, `"main"`) {
		t.Fatalf("config_error = %v", e)
	}
}

func TestLoadDropsAppliedInboundsThatAreNotOnDisk(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	if err := writeJSONAtomic(r.cfg.NodeStatePath, nodeState{Applied: mainIn}); err != nil {
		t.Fatal(err)
	}
	n := r.newNode()
	if err := n.Load(); err != nil {
		t.Fatal(err)
	}
	if n.state.Applied != nil {
		t.Fatal("state claims inbounds that xray does not run")
	}
}

func TestRunningTagsReadsConfigAndPrevious(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	if tags := r.node.RunningTags(); len(tags) != 0 {
		t.Fatalf("frame has no inbounds, got %v", tags)
	}
	if err := writeFileAtomic(r.cfg.XrayConfig, []byte(`{"inbounds":[{"tag":"vless-in"},{"port":1},{"tag":"alpha"}]}`)); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(previousPath(r.cfg.XrayConfig), []byte(`{"inbounds":[{"tag":"alpha"},{"tag":"old"}]}`)); err != nil {
		t.Fatal(err)
	}
	if tags := r.node.RunningTags(); !reflect.DeepEqual(tags, []string{"vless-in", "alpha", "old"}) {
		t.Fatalf("tags = %v", tags)
	}
	if err := writeFileAtomic(r.cfg.XrayConfig, []byte(`{"inbounds": [ // jsonc`)); err != nil {
		t.Fatal(err)
	}
	if tags := r.node.RunningTags(); !reflect.DeepEqual(tags, []string{"alpha", "old"}) {
		t.Fatalf("tags = %v", tags)
	}
	if !strings.Contains(r.logs.String(), "xray_config_unparsable") {
		t.Fatal("unreadable config must be logged: revocation cannot reach its inbounds")
	}
}

func TestApplyNeverLeaksPrivateKey(t *testing.T) {
	r := newNodeRig(t, newFakeXray())
	ctx := context.Background()
	r.node.Apply(ctx, mainIn)
	r.node.Apply(ctx, brokenInbounds())
	bad := testInbound("main")
	bad.Reality.Dest = "127.0.0.1:2019"
	r.node.Apply(ctx, []Inbound{bad})
	for _, secret := range []string{testPrivateKey, brokenKey} {
		if strings.Contains(r.logs.String(), secret) {
			t.Fatalf("private key in logs: %s", r.logs.String())
		}
		if e := r.node.ConfigError(); e != nil && strings.Contains(*e, secret) {
			t.Fatalf("private key in config_error: %s", *e)
		}
	}
}
