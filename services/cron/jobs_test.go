package cron

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ITG-Labs/sylph/pkg/apiclient"
	"github.com/ITG-Labs/sylph/pkg/logging"
)

func fakeCore(t *testing.T, h http.HandlerFunc) *CoreClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	api, err := apiclient.New("cron", srv.URL, nil, logging.NewWithWriter("cron", "error", io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return NewCoreClient(api)
}

func attrsOf(attrs []slog.Attr) map[string]int64 {
	out := map[string]int64{}
	for _, a := range attrs {
		out[a.Key] = a.Value.Int64()
	}
	return out
}

func TestJobsCallMaintenanceEndpoints(t *testing.T) {
	var cleanupBody map[string]any
	core := fakeCore(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		switch r.URL.Path {
		case "/v1/maintenance/expire":
			_, _ = io.WriteString(w, `{"expired":4}`)
		case "/v1/maintenance/cleanup":
			_ = json.NewDecoder(r.Body).Decode(&cleanupBody)
			_, _ = io.WriteString(w, `{"deltas":120000,"batches":3000,"keys":2}`)
		default:
			http.NotFound(w, r)
		}
	})
	jobs := Jobs(core, &Config{ExpireEvery: time.Minute, CleanupEvery: time.Hour, RetentionDays: 45})
	if len(jobs) != 2 || jobs[0].Name != "expire" || jobs[0].Every != time.Minute || jobs[1].Name != "cleanup" || jobs[1].Every != time.Hour {
		t.Fatalf("jobs = %+v", jobs)
	}
	attrs, err := jobs[0].Do(context.Background())
	if err != nil || attrsOf(attrs)["expired"] != 4 {
		t.Fatalf("expire = %v, %v", attrs, err)
	}
	attrs, err = jobs[1].Do(context.Background())
	got := attrsOf(attrs)
	if err != nil || got["deltas"] != 120000 || got["batches"] != 3000 || got["keys"] != 2 {
		t.Fatalf("cleanup = %v, %v", attrs, err)
	}
	if len(cleanupBody) != 1 || cleanupBody["older_than_days"] != float64(45) {
		t.Fatalf("cleanup body = %v", cleanupBody)
	}
}

func TestJobsReportCoreErrors(t *testing.T) {
	core := fakeCore(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"code":"forbidden","message":"role cron is not allowed"}`)
	})
	for _, job := range Jobs(core, &Config{ExpireEvery: time.Minute, CleanupEvery: time.Hour, RetentionDays: 30}) {
		if _, err := job.Do(context.Background()); err == nil {
			t.Fatalf("%s swallowed a 403", job.Name)
		}
	}
}
