package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

type fakeCore struct {
	mu              sync.Mutex
	syncVersion     int64
	syncUsers       []SyncUser
	syncInbounds    []Inbound
	deltasSeen      []Delta
	batchAttempts   []string
	batchesSeen     map[string]bool
	heartbeats      int
	lastHeartbeat   map[string]any
	failDeltas      bool
	failAfterCommit bool
	syncExtra       map[string]any
	perInbound      bool
}

func (f *fakeCore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/sync/users":
		var body struct {
			KnownVersion int64 `json:"known_version"`
			PerInbound   bool  `json:"per_inbound"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.perInbound = body.PerInbound
		if body.KnownVersion == f.syncVersion {
			json.NewEncoder(w).Encode(map[string]any{"version": f.syncVersion})
			return
		}
		resp := map[string]any{"version": f.syncVersion, "users": f.syncUsers, "inbounds": f.syncInbounds}
		for k, v := range f.syncExtra {
			resp[k] = v
		}
		json.NewEncoder(w).Encode(resp)
	case "/v1/sync/deltas":
		var body Batch
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		f.batchAttempts = append(f.batchAttempts, body.ID)
		if f.failDeltas {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if f.batchesSeen == nil {
			f.batchesSeen = map[string]bool{}
		}
		duplicate := f.batchesSeen[body.ID]
		if !duplicate {
			f.batchesSeen[body.ID] = true
			f.deltasSeen = append(f.deltasSeen, body.Deltas...)
		}
		if f.failAfterCommit {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"accepted": true, "duplicate": duplicate})
	case "/v1/sync/heartbeat":
		f.heartbeats++
		f.lastHeartbeat = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&f.lastHeartbeat)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

const testPrivateKey = "8F0ET9EsN7sZiyXEsjQ2FnD1ew8BagsJ-BMWKb_s1nk"

func testInbound(tag string) Inbound {
	return Inbound{
		Tag: tag, Port: 443, Protocol: "vless", Transport: "tcp", Security: "reality", Flow: "xtls-rprx-vision",
		Reality: &Reality{
			Dest:        "www.example.com:443",
			ServerNames: []string{tag + ".example.com"},
			ShortIDs:    []string{"a1b2c3d4e5f60718"},
			Fingerprint: "chrome",
			PrivateKey:  testPrivateKey,
		},
	}
}

func testInbounds(tags ...string) []Inbound {
	out := make([]Inbound, 0, len(tags))
	for _, tag := range tags {
		out = append(out, testInbound(tag))
	}
	return out
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func httptestOf(fc *fakeCore, t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(fc)
	t.Cleanup(ts.Close)
	return ts
}

func testCoreClient(t *testing.T, fc *fakeCore) *CoreClient {
	t.Helper()
	ts := httptestOf(fc, t)
	cfg := &Config{CoreURL: ts.URL}
	return newCoreClientForTest(cfg, discardLogger())
}

func TestSyncUsersFull(t *testing.T) {
	fc := &fakeCore{syncVersion: 5, syncUsers: []SyncUser{{UUID: "u-1"}, {UUID: "u-2"}}, syncInbounds: testInbounds("main")}
	cc := testCoreClient(t, fc)
	resp, err := cc.SyncUsers(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Version != 5 || len(resp.Users) != 2 || resp.Users[0].UUID != "u-1" {
		t.Fatalf("resp = %+v", resp)
	}
	if !reflect.DeepEqual(resp.Inbounds, testInbounds("main")) {
		t.Fatalf("inbounds = %+v", resp.Inbounds)
	}
}

func TestSyncInboundsWireFormat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"version":3,"users":[],"inbounds":[{"tag":"main","port":443,"protocol":"vless","transport":"tcp","security":"reality","flow":"xtls-rprx-vision","reality":{"dest":"www.example.com:443","server_names":["www.example.com"],"short_ids":["a1b2"],"fingerprint":"chrome","private_key":"k"}}]}`)
	}))
	t.Cleanup(ts.Close)
	cc := newCoreClientForTest(&Config{CoreURL: ts.URL}, discardLogger())
	resp, err := cc.SyncUsers(context.Background(), -1)
	if err != nil {
		t.Fatal(err)
	}
	want := []Inbound{{Tag: "main", Port: 443, Protocol: "vless", Transport: "tcp", Security: "reality", Flow: "xtls-rprx-vision",
		Reality: &Reality{Dest: "www.example.com:443", ServerNames: []string{"www.example.com"}, ShortIDs: []string{"a1b2"}, Fingerprint: "chrome", PrivateKey: "k"}}}
	if !reflect.DeepEqual(resp.Inbounds, want) {
		t.Fatalf("inbounds = %+v", resp.Inbounds)
	}
}

func TestSyncEmptyInboundsAreKnown(t *testing.T) {
	fc := &fakeCore{syncVersion: 2, syncUsers: []SyncUser{}, syncInbounds: []Inbound{}}
	resp, err := testCoreClient(t, fc).SyncUsers(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Inbounds == nil || len(resp.Inbounds) != 0 {
		t.Fatalf("empty inbounds list decoded as %#v, want known and empty", resp.Inbounds)
	}
}

func TestSyncUsersNoChange(t *testing.T) {
	fc := &fakeCore{syncVersion: 5}
	cc := testCoreClient(t, fc)
	resp, err := cc.SyncUsers(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Users != nil || resp.Inbounds != nil {
		t.Fatalf("no-change sync must carry neither users nor inbounds, got %+v", resp)
	}
}

func TestReportDeltas(t *testing.T) {
	fc := &fakeCore{}
	cc := testCoreClient(t, fc)
	batch := &Batch{ID: "b-1", Deltas: []Delta{{KeyUUID: "u-1", BytesUp: 100, BytesDown: 200}}}
	dup, err := cc.ReportDeltas(context.Background(), batch)
	if err != nil || dup {
		t.Fatalf("dup = %v, err = %v", dup, err)
	}
	if len(fc.deltasSeen) != 1 || fc.deltasSeen[0].BytesUp != 100 {
		t.Fatalf("deltas = %+v", fc.deltasSeen)
	}
	if len(fc.batchAttempts) != 1 || fc.batchAttempts[0] != "b-1" {
		t.Fatalf("batch ids = %v", fc.batchAttempts)
	}
	dup, err = cc.ReportDeltas(context.Background(), batch)
	if err != nil || !dup {
		t.Fatalf("repeat: dup = %v, err = %v; want duplicate", dup, err)
	}
}

func TestHeartbeat(t *testing.T) {
	fc := &fakeCore{}
	cc := testCoreClient(t, fc)
	users := 7
	hb := Heartbeat{AgentVersion: "0.1.0", XrayVersion: "25.1.1", AgentUptimeSec: 30, ActiveUsers: &users}
	if err := cc.Heartbeat(context.Background(), hb); err != nil {
		t.Fatal(err)
	}
	if fc.heartbeats != 1 {
		t.Fatalf("heartbeats = %d", fc.heartbeats)
	}
	got := fc.lastHeartbeat
	if got["agent_version"] != "0.1.0" || got["xray_version"] != "25.1.1" || got["agent_uptime_sec"] != float64(30) || got["active_users"] != float64(7) {
		t.Fatalf("heartbeat body = %v", got)
	}
	if v, ok := got["xray_uptime_sec"]; !ok || v != nil {
		t.Fatalf("unknown xray uptime must be sent as null, got %v (present %v)", v, ok)
	}
}
