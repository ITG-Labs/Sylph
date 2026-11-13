package cron

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func zipOf(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, "x")
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func geoServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/latest/sing-box.zip", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/download/202611131125/sing-box.zip", http.StatusFound)
	})
	mux.HandleFunc("/releases/download/202611131125/sing-box.zip", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/blob/abc?sig=1", http.StatusFound)
	})
	mux.HandleFunc("/blob/abc", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchGeoTags(t *testing.T) {
	srv := geoServer(t, zipOf(t, "rule-set-geosite/", "rule-set-geosite/geosite-category-ru.srs", "rule-set-geoip/geoip-ru.srs", "readme.txt", "rule-set-geosite/geosite-google@cn.srs"))
	tags, err := fetchGeoTags(context.Background(), &http.Client{Timeout: 10 * time.Second}, srv.URL+"/latest/sing-box.zip", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if tags.Release != "202611131125" || !slices.Equal(tags.Geosite, []string{"category-ru", "google@cn"}) || !slices.Equal(tags.Geoip, []string{"ru"}) {
		t.Fatalf("tags = %+v", tags)
	}
}

func TestFetchGeoTagsTooLarge(t *testing.T) {
	srv := geoServer(t, bytes.Repeat([]byte("x"), 2048))
	_, err := fetchGeoTags(context.Background(), http.DefaultClient, srv.URL+"/latest/sing-box.zip", 1024)
	if err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchGeoTagsEmpty(t *testing.T) {
	srv := geoServer(t, zipOf(t, "readme.txt", "rule-set-geosite/geosite-a.srs"))
	if _, err := fetchGeoTags(context.Background(), http.DefaultClient, srv.URL+"/latest/sing-box.zip", 1<<20); err == nil {
		t.Fatal("a zip without geoip rule sets must fail")
	}
}

func TestFetchGeoTagsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	if _, err := fetchGeoTags(context.Background(), http.DefaultClient, srv.URL+"/x.zip", 1<<20); err == nil {
		t.Fatal("404 must fail")
	}
}

func TestGeoTagsJobPostsToCore(t *testing.T) {
	src := geoServer(t, zipOf(t, "rule-set-geosite/geosite-youtube.srs", "rule-set-geoip/geoip-ru.srs"))
	var got map[string]any
	core := fakeCore(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/maintenance/geo-tags" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"changed":true}`)
	})
	jobs := Jobs(core, &Config{ExpireEvery: time.Minute, CleanupEvery: time.Hour, PaymentsEvery: time.Minute, GeoTagsEvery: 24 * time.Hour, GeoTagsURL: src.URL + "/latest/sing-box.zip"})
	job := jobs[len(jobs)-1]
	if job.Name != "geotags" || job.Every != 24*time.Hour {
		t.Fatalf("job = %+v", job)
	}
	attrs, err := job.Do(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["release"] != "202611131125" || attrsOf(attrs)["geosite"] != 1 || attrsOf(attrs)["geoip"] != 1 {
		t.Fatalf("posted %v, attrs %v", got, attrs)
	}
}
