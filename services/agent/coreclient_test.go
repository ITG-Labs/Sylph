package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type fakeCore struct {
	mu              sync.Mutex
	syncVersion     int64
	syncUsers       []SyncUser
	deltasSeen      []Delta
	batchAttempts   []string
	batchesSeen     map[string]bool
	heartbeats      int
	failDeltas      bool
	failAfterCommit bool
}

func (f *fakeCore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/sync/users":
		var body struct {
			KnownVersion int64 `json:"known_version"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.KnownVersion == f.syncVersion {
			json.NewEncoder(w).Encode(map[string]any{"version": f.syncVersion})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"version": f.syncVersion, "users": f.syncUsers, "config": "{}"})
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
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
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
	cfg := &Config{CoreURL: ts.URL, InboundTag: "vless-in"}
	return newCoreClientForTest(cfg, discardLogger())
}

func TestSyncUsersFull(t *testing.T) {
	fc := &fakeCore{syncVersion: 5, syncUsers: []SyncUser{{UUID: "u-1"}, {UUID: "u-2"}}}
	cc := testCoreClient(t, fc)
	resp, err := cc.SyncUsers(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Version != 5 || len(resp.Users) != 2 || resp.Users[0].UUID != "u-1" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestSyncUsersNoChange(t *testing.T) {
	fc := &fakeCore{syncVersion: 5}
	cc := testCoreClient(t, fc)
	resp, err := cc.SyncUsers(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Users != nil {
		t.Fatalf("users should be nil, got %+v", resp.Users)
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
	if err := cc.Heartbeat(context.Background(), "0.1.0", "25.1.1"); err != nil {
		t.Fatal(err)
	}
	if fc.heartbeats != 1 {
		t.Fatalf("heartbeats = %d", fc.heartbeats)
	}
}
