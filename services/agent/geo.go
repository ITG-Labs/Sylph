package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ITG-Labs/sylph/pkg/netaddr"
	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/agent/internal/xrayapi/geodata"
)

const (
	defaultGeoMaxBytes = 200 << 20
	geoChecksumMax     = 4 << 10
	geoDownloadTimeout = 5 * time.Minute
	geoRetryPause      = 5 * time.Minute
)

type geoEntry struct {
	URL          string    `json:"url"`
	SHA256URL    string    `json:"sha256_url"`
	Generation   int64     `json:"generation"`
	SHA256       string    `json:"sha256,omitempty"`
	Size         int64     `json:"size,omitempty"`
	DownloadedAt time.Time `json:"downloaded_at,omitzero"`
	Error        string    `json:"error,omitempty"`
	TriedAt      time.Time `json:"tried_at,omitzero"`
}

type GeoManager struct {
	dir       string
	statePath string
	client    *http.Client
	allowHost func(string) bool
	logger    *slog.Logger
	maxBytes  int64
	retry     time.Duration
	now       func() time.Time
	state     map[string]geoEntry
}

func NewGeoManager(dir, statePath string, logger *slog.Logger) *GeoManager {
	return &GeoManager{
		dir:       dir,
		statePath: statePath,
		client:    &http.Client{},
		allowHost: netaddr.PublicHost,
		logger:    logger,
		maxBytes:  defaultGeoMaxBytes,
		retry:     geoRetryPause,
		now:       time.Now,
		state:     map[string]geoEntry{},
	}
}

func (g *GeoManager) Load() error {
	data, err := os.ReadFile(g.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read geo state: %w", err)
	}
	state := map[string]geoEntry{}
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse geo state: %w", err)
	}
	g.state = state
	return nil
}

func (g *GeoManager) hostAllowed(host string) bool {
	if g.allowHost != nil {
		return g.allowHost(host)
	}
	return netaddr.PublicHost(host)
}

func (g *GeoManager) path(name string) string {
	return filepath.Join(g.dir, name+".dat")
}

func (g *GeoManager) Ensure(ctx context.Context, geo *SyncGeo) {
	if geo == nil {
		return
	}
	changed := false
	for _, src := range geo.Sources {
		e, known := g.state[src.Name]
		_, statErr := os.Stat(g.path(src.Name))
		exists := statErr == nil && noderoute.SourcePattern.MatchString(src.Name)
		sameSource := known && e.URL == src.URL && e.SHA256URL == src.SHA256URL
		if exists && sameSource && e.Generation >= geo.Generation {
			continue
		}
		if !exists && sameSource && e.Error != "" && g.now().Sub(e.TriedAt) < g.retry {
			continue
		}
		sum, size, err := g.download(ctx, src)
		next := geoEntry{URL: src.URL, SHA256URL: src.SHA256URL, Generation: geo.Generation, TriedAt: g.now()}
		if err != nil {
			next.Error = truncate(err.Error(), 300)
			if exists {
				next.SHA256, next.Size, next.DownloadedAt = e.SHA256, e.Size, e.DownloadedAt
			}
			g.logger.Error("geo_download_failed", "source", src.Name, "error", next.Error)
		} else {
			next.SHA256, next.Size, next.DownloadedAt = sum, size, g.now()
			g.logger.Info("geo_downloaded", "source", src.Name, "size", size, "sha256", sum[:12])
		}
		g.state[src.Name] = next
		changed = true
	}
	if changed {
		if err := writeJSONAtomic(g.statePath, g.state); err != nil {
			g.logger.Error("geo_state_save_failed", "error", err.Error())
		}
	}
}

func (g *GeoManager) checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return errors.New("only https links are allowed")
	}
	if !g.hostAllowed(u.Hostname()) {
		return errors.New("the link points to an internal host")
	}
	return nil
}

func (g *GeoManager) fetch(ctx context.Context, raw string, limit int64, w io.Writer) (int64, error) {
	if err := g.checkURL(raw); err != nil {
		return 0, err
	}
	client := *g.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return g.checkURL(req.URL.String())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download answered %d", resp.StatusCode)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, fmt.Errorf("file is too big (over %d bytes)", limit)
	}
	return n, nil
}

func validGeoFile(kind string, data []byte) error {
	switch kind {
	case "domain":
		var list geodata.GeoSiteList
		if err := proto.Unmarshal(data, &list); err != nil || !siteListOK(&list) {
			return errors.New("the file is not a geosite list")
		}
	case "ip":
		var list geodata.GeoIPList
		if err := proto.Unmarshal(data, &list); err != nil || !ipListOK(&list) {
			return errors.New("the file is not a geoip list")
		}
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
	return nil
}

func (g *GeoManager) download(ctx context.Context, src GeoSource) (string, int64, error) {
	if !noderoute.SourcePattern.MatchString(src.Name) {
		return "", 0, errors.New("bad source name")
	}
	ctx, cancel := context.WithTimeout(ctx, geoDownloadTimeout)
	defer cancel()
	if err := os.MkdirAll(g.dir, 0o755); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(g.dir, "."+src.Name+"-*.tmp")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	hash := sha256.New()
	size, err := g.fetch(ctx, src.URL, g.maxBytes, io.MultiWriter(tmp, hash))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", 0, err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if src.SHA256URL != "" {
		var buf strings.Builder
		if _, err := g.fetch(ctx, src.SHA256URL, geoChecksumMax, &buf); err != nil {
			return "", 0, fmt.Errorf("sha256 file: %w", err)
		}
		want := ""
		if fields := strings.Fields(buf.String()); len(fields) > 0 {
			want = strings.ToLower(fields[0])
		}
		if want != sum {
			return "", 0, fmt.Errorf("sha256 mismatch: the file is %s, the checksum says %s", sum[:12], truncate(want, 12))
		}
	}
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return "", 0, err
	}
	if err := validGeoFile(src.Kind, data); err != nil {
		return "", 0, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp.Name(), g.path(src.Name)); err != nil {
		return "", 0, err
	}
	return sum, size, syncDir(g.dir)
}

func (g *GeoManager) Status() map[string]GeoStatus {
	out := map[string]GeoStatus{}
	for name, e := range g.state {
		st := GeoStatus{SHA256: e.SHA256, Size: e.Size, Error: e.Error}
		if !e.DownloadedAt.IsZero() {
			st.DownloadedAt = e.DownloadedAt.UTC().Format(time.RFC3339)
		}
		out[name] = st
	}
	return out
}

func (g *GeoManager) Files(names []string) (missing []string, sums []string, newest time.Time) {
	for _, name := range names {
		info, err := os.Stat(g.path(name))
		if err != nil || !noderoute.SourcePattern.MatchString(name) {
			missing = append(missing, name)
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		sums = append(sums, name+":"+g.state[name].SHA256)
	}
	slices.Sort(sums)
	return missing, sums, newest
}

func siteListOK(list *geodata.GeoSiteList) bool {
	for _, e := range list.GetEntry() {
		if e.GetCode() == "" {
			return false
		}
		for _, d := range e.GetDomain() {
			if d.GetValue() != "" {
				return true
			}
		}
	}
	return false
}

func ipListOK(list *geodata.GeoIPList) bool {
	for _, e := range list.GetEntry() {
		if e.GetCode() == "" {
			return false
		}
		for _, c := range e.GetCidr() {
			if n := len(c.GetIp()); n == 4 || n == 16 {
				return true
			}
		}
	}
	return false
}
