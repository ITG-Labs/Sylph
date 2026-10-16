package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
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
	geoMaxEntry        = 100 << 20
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
	mu        sync.Mutex
	busy      atomic.Bool
	state     map[string]geoEntry
	wanted    []string
}

func NewGeoManager(dir, statePath string, logger *slog.Logger) *GeoManager {
	return &GeoManager{
		dir:       dir,
		statePath: statePath,
		client:    publicClient(nil, publicAddr),
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
	g.mu.Lock()
	g.state = state
	g.mu.Unlock()
	return nil
}

func (g *GeoManager) Start(ctx context.Context, geo *SyncGeo, need map[string][]string) {
	if geo == nil || !g.busy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer g.busy.Store(false)
		g.Ensure(ctx, geo, need)
	}()
}

func (g *GeoManager) entry(name string) (geoEntry, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.state[name]
	return e, ok
}

func publicAddr(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil || netaddr.Internal(netaddr.CanonicalHost(host)) {
		return false
	}
	addr = addr.WithZone("").Unmap()
	for _, p := range privateNets {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

func publicClient(tlsConfig *tls.Config, allow func(string) bool) *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if !allow(host) {
			return fmt.Errorf("%s is an internal address", host)
		}
		return nil
	}}
	return &http.Client{Transport: &http.Transport{
		DialContext:           dialer.DialContext,
		TLSClientConfig:       tlsConfig,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		ForceAttemptHTTP2:     true,
	}}
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

func (g *GeoManager) Ensure(ctx context.Context, geo *SyncGeo, need map[string][]string) {
	if geo == nil {
		return
	}
	wanted := make([]string, 0, len(geo.Sources))
	for _, src := range geo.Sources {
		wanted = append(wanted, src.Name)
	}
	g.mu.Lock()
	g.wanted = wanted
	g.mu.Unlock()
	changed := false
	for _, src := range geo.Sources {
		if ctx.Err() != nil {
			break
		}
		e, known := g.entry(src.Name)
		_, statErr := os.Stat(g.path(src.Name))
		exists := statErr == nil && noderoute.SourcePattern.MatchString(src.Name)
		sameSource := known && e.URL == src.URL && e.SHA256URL == src.SHA256URL
		if exists && sameSource && e.Generation >= geo.Generation {
			continue
		}
		if !exists && sameSource && e.Error != "" && g.now().Sub(e.TriedAt) < g.retry {
			continue
		}
		sum, size, err := g.download(ctx, src, need[src.Name])
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
		g.mu.Lock()
		g.state[src.Name] = next
		g.mu.Unlock()
		changed = true
	}
	if changed {
		g.mu.Lock()
		err := writeJSONAtomic(g.statePath, g.state)
		g.mu.Unlock()
		if err != nil {
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

func scanGeoFile(kind string, path string) (map[string]bool, error) {
	if kind != "domain" && kind != "ip" {
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	bad := errors.New("the file is not a geosite list")
	if kind == "ip" {
		bad = errors.New("the file is not a geoip list")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<16)
	codes := map[string]bool{}
	filled := false
	var buf []byte
	for {
		key, err := binary.ReadUvarint(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, bad
		}
		if num, typ := protowire.DecodeTag(key); num != 1 || typ != protowire.BytesType {
			return nil, bad
		}
		n, err := binary.ReadUvarint(r)
		if err != nil || n > geoMaxEntry {
			return nil, bad
		}
		if uint64(cap(buf)) < n {
			buf = make([]byte, n)
		}
		buf = buf[:n]
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, bad
		}
		code, ok, has := scanEntry(kind, buf)
		if !ok {
			return nil, bad
		}
		filled = filled || has
		codes[strings.ToUpper(code)] = true
	}
	if !filled {
		return nil, bad
	}
	return codes, nil
}

func (g *GeoManager) download(ctx context.Context, src GeoSource, need []string) (string, int64, error) {
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
	codes, err := scanGeoFile(src.Kind, tmp.Name())
	if err != nil {
		return "", 0, err
	}
	var missing []string
	for _, code := range need {
		if !codes[strings.ToUpper(code)] {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		return "", 0, fmt.Errorf("the new file has no tags %s that the rules use: the old file is kept", strings.Join(missing, ", "))
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return "", 0, err
	}
	now := time.Now()
	if err := os.Chtimes(tmp.Name(), now, now); err != nil {
		return "", 0, err
	}
	path := g.path(src.Name)
	g.mu.Lock()
	defer g.mu.Unlock()
	_ = os.Remove(path + ".previous")
	if err := os.Link(path, path+".previous"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", 0, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", 0, err
	}
	return sum, size, syncDir(g.dir)
}

func (g *GeoManager) Restore(names []string, reason string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	restored := false
	for _, name := range names {
		path := g.path(name)
		if err := os.Rename(path+".previous", path); err != nil {
			continue
		}
		restored = true
		e := g.state[name]
		e.Error = truncate(reason, 300)
		if data, err := os.ReadFile(path); err == nil {
			s := sha256.Sum256(data)
			e.SHA256, e.Size = hex.EncodeToString(s[:]), int64(len(data))
		}
		g.state[name] = e
		g.logger.Error("geo_restored", "source", name, "reason", e.Error)
	}
	if restored {
		_ = syncDir(g.dir)
		if err := writeJSONAtomic(g.statePath, g.state); err != nil {
			g.logger.Error("geo_state_save_failed", "error", err.Error())
		}
	}
	return restored
}

func (g *GeoManager) Commit(names []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, name := range names {
		_ = os.Remove(g.path(name) + ".previous")
	}
}

func (g *GeoManager) Status() map[string]GeoStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]GeoStatus{}
	for name, e := range g.state {
		if g.wanted != nil && !slices.Contains(g.wanted, name) {
			continue
		}
		st := GeoStatus{SHA256: e.SHA256, Size: e.Size, Error: e.Error}
		if !e.DownloadedAt.IsZero() {
			st.DownloadedAt = e.DownloadedAt.UTC().Format(time.RFC3339)
		}
		out[name] = st
	}
	return out
}

func (g *GeoManager) Files(names []string) (missing []string, sums []string, newest time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
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

func scanEntry(kind string, buf []byte) (code string, ok bool, filled bool) {
	if kind == "domain" {
		var e geodata.GeoSite
		if proto.Unmarshal(buf, &e) != nil || e.GetCode() == "" {
			return "", false, false
		}
		for _, d := range e.GetDomain() {
			if d.GetValue() != "" {
				return e.GetCode(), true, true
			}
		}
		return e.GetCode(), true, false
	}
	var e geodata.GeoIP
	if proto.Unmarshal(buf, &e) != nil || e.GetCode() == "" {
		return "", false, false
	}
	for _, c := range e.GetCidr() {
		if n := len(c.GetIp()); n == 4 || n == 16 {
			return e.GetCode(), true, true
		}
	}
	return e.GetCode(), true, false
}
