package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/geodata"
)

func siteDat(t *testing.T, domain string) []byte {
	t.Helper()
	data, err := proto.Marshal(&geodata.GeoSiteList{Entry: []*geodata.GeoSite{{Code: "RU", Domain: []*geodata.Domain{{Type: geodata.Domain_Domain, Value: domain}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func ipDat(t *testing.T) []byte {
	t.Helper()
	data, err := proto.Marshal(&geodata.GeoIPList{Entry: []*geodata.GeoIP{{Code: "RU", Cidr: []*geodata.CIDR{{Ip: []byte{5, 0, 0, 0}, Prefix: 8}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type geoServer struct {
	*httptest.Server
	files map[string][]byte
	hits  map[string]*atomic.Int32
}

func newGeoServer(t *testing.T, files map[string][]byte) *geoServer {
	t.Helper()
	g := &geoServer{files: files, hits: map[string]*atomic.Int32{}}
	for name := range files {
		g.hits[name] = &atomic.Int32{}
	}
	g.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if target, ok := strings.CutPrefix(r.URL.Path, "/redirect/"); ok {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		data, ok := g.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		g.hits[name].Add(1)
		_, _ = w.Write(data)
	}))
	t.Cleanup(g.Close)
	return g
}

func testGeo(t *testing.T, srv *geoServer) *GeoManager {
	t.Helper()
	dir := t.TempDir()
	g := NewGeoManager(filepath.Join(dir, "geo"), filepath.Join(dir, "geo.json"), discardLogger())
	g.client = srv.Client()
	g.allowHost = func(string) bool { return true }
	return g
}

func sumOf(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func TestGeoDownloadsOnceAndAgainOnRequest(t *testing.T) {
	site := siteDat(t, "example.ru")
	srv := newGeoServer(t, map[string][]byte{"ru.dat": site, "ru.dat.sha256sum": []byte(sumOf(site) + "  ru.dat\n"), "ip.dat": ipDat(t)})
	g := testGeo(t, srv)
	ctx := context.Background()
	geo := &SyncGeo{Generation: 1, Sources: []GeoSource{
		{Name: "ru", Kind: "domain", URL: srv.URL + "/ru.dat", SHA256URL: srv.URL + "/ru.dat.sha256sum"},
		{Name: "ips", Kind: "ip", URL: srv.URL + "/ip.dat"},
	}}
	g.Ensure(ctx, geo)
	got, err := os.ReadFile(filepath.Join(g.dir, "ru.dat"))
	if err != nil || string(got) != string(site) {
		t.Fatalf("file = %v", err)
	}
	st := g.Status()
	if st["ru"].SHA256 != sumOf(site) || st["ru"].Size != int64(len(site)) || st["ru"].Error != "" || st["ips"].Error != "" {
		t.Fatalf("status = %+v", st)
	}
	g.Ensure(ctx, geo)
	if srv.hits["ru.dat"].Load() != 1 {
		t.Fatal("unchanged source downloaded again")
	}
	geo.Generation = 2
	g.Ensure(ctx, geo)
	if srv.hits["ru.dat"].Load() != 2 || srv.hits["ip.dat"].Load() != 2 {
		t.Fatal("the refresh button did not download again")
	}
	reloaded := NewGeoManager(g.dir, g.statePath, discardLogger())
	reloaded.client, reloaded.allowHost = g.client, g.allowHost
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	reloaded.Ensure(ctx, geo)
	if srv.hits["ru.dat"].Load() != 2 {
		t.Fatal("state was not kept across restarts")
	}
	missing, sums, newest := reloaded.Files([]string{"ru", "ips", "absent"})
	if len(missing) != 1 || missing[0] != "absent" || len(sums) != 2 || newest.IsZero() {
		t.Fatalf("files = %v %v %v", missing, sums, newest)
	}
}

func TestGeoRejectsBadDownloads(t *testing.T) {
	site := siteDat(t, "example.ru")
	srv := newGeoServer(t, map[string][]byte{
		"ru.dat": site, "bad.sum": []byte(strings.Repeat("0", 64) + "  ru.dat\n"),
		"page.html": []byte("<html>rate limited</html>"), "big.dat": make([]byte, 2048), "ip.dat": ipDat(t),
	})
	ctx := context.Background()
	for _, c := range []struct {
		name string
		src  GeoSource
		want string
	}{
		{"checksum", GeoSource{Name: "a", Kind: "domain", URL: srv.URL + "/ru.dat", SHA256URL: srv.URL + "/bad.sum"}, "sha256"},
		{"not a geo file", GeoSource{Name: "a", Kind: "domain", URL: srv.URL + "/page.html"}, "not a geosite"},
		{"wrong kind", GeoSource{Name: "a", Kind: "domain", URL: srv.URL + "/ip.dat"}, "not a geosite"},
		{"too big", GeoSource{Name: "a", Kind: "domain", URL: srv.URL + "/big.dat"}, "too big"},
		{"plain http", GeoSource{Name: "a", Kind: "domain", URL: strings.Replace(srv.URL, "https", "http", 1) + "/ru.dat"}, "https"},
		{"redirect to http", GeoSource{Name: "a", Kind: "domain", URL: srv.URL + "/redirect/" + strings.Replace(srv.URL, "https", "http", 1) + "/ru.dat"}, "https"},
		{"bad name", GeoSource{Name: "../x", Kind: "domain", URL: srv.URL + "/ru.dat"}, "name"},
		{"missing", GeoSource{Name: "a", Kind: "domain", URL: srv.URL + "/nope.dat"}, "404"},
	} {
		g := testGeo(t, srv)
		g.maxBytes = 1024
		g.Ensure(ctx, &SyncGeo{Generation: 1, Sources: []GeoSource{c.src}})
		st := g.Status()[c.src.Name]
		if !strings.Contains(st.Error, c.want) {
			t.Errorf("%s: error %q, want %q", c.name, st.Error, c.want)
		}
		if missing, _, _ := g.Files([]string{c.src.Name}); len(missing) != 1 {
			t.Errorf("%s: a bad file was written", c.name)
		}
	}
	g := testGeo(t, srv)
	if g.allowHost = nil; g.hostAllowed("127.0.0.1") || g.hostAllowed("localhost") || !g.hostAllowed("example.com") {
		t.Fatal("default host check lets internal hosts through")
	}
}

func TestGeoRetriesAMissingFileAfterAPause(t *testing.T) {
	srv := newGeoServer(t, map[string][]byte{})
	g := testGeo(t, srv)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }
	ctx := context.Background()
	geo := &SyncGeo{Generation: 1, Sources: []GeoSource{{Name: "ru", Kind: "domain", URL: srv.URL + "/ru.dat"}}}
	g.Ensure(ctx, geo)
	srv.files["ru.dat"] = siteDat(t, "example.ru")
	srv.hits["ru.dat"] = &atomic.Int32{}
	g.Ensure(ctx, geo)
	if srv.hits["ru.dat"].Load() != 0 {
		t.Fatal("retried a failed download without a pause")
	}
	now = now.Add(g.retry + time.Second)
	g.Ensure(ctx, geo)
	if missing, _, _ := g.Files([]string{"ru"}); len(missing) != 0 || g.Status()["ru"].Error != "" {
		t.Fatalf("retry after the pause failed: %+v", g.Status())
	}
}

func TestGeoRefusesNamesThatResolveToInternalAddresses(t *testing.T) {
	srv := newGeoServer(t, map[string][]byte{"ru.dat": siteDat(t, "example.ru")})
	tlsConfig := srv.Client().Transport.(*http.Transport).TLSClientConfig
	geo := &SyncGeo{Generation: 1, Sources: []GeoSource{{Name: "ru", Kind: "domain", URL: srv.URL + "/ru.dat"}}}
	g := testGeo(t, srv)
	g.client = publicClient(tlsConfig, publicAddr)
	g.Ensure(context.Background(), geo)
	if st := g.Status()["ru"]; !strings.Contains(st.Error, "internal address") || srv.hits["ru.dat"].Load() != 0 {
		t.Fatalf("a name that resolves to 127.0.0.1 was fetched: %+v", st)
	}
	g = testGeo(t, srv)
	g.client = publicClient(tlsConfig, func(string) bool { return true })
	g.Ensure(context.Background(), geo)
	if st := g.Status()["ru"]; st.Error != "" || st.SHA256 == "" {
		t.Fatalf("the guarded client cannot download at all: %+v", st)
	}
	for _, host := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "198.18.0.5", "::1", "::ffff:127.0.0.1", "fe80::1", "fd00::1", "64:ff9b::a00:1", "0.0.0.0"} {
		if publicAddr(host) {
			t.Errorf("%s passes as public", host)
		}
	}
	if !publicAddr("1.1.1.1") || !publicAddr("2606:4700::1111") {
		t.Error("public addresses are refused")
	}
	if NewGeoManager(t.TempDir(), "", discardLogger()).client.Transport.(*http.Transport).Proxy != nil {
		t.Error("downloads may go through a proxy from the environment")
	}
}
