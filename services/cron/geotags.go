package cron

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/clientroute"
)

const (
	DefaultGeoTagsURL = "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/sing-box.zip"
	maxGeoTagsZip     = 96 << 20
)

var releasePath = regexp.MustCompile(`/releases/download/([^/]+)/[^/]+$`)

func fetchGeoTags(ctx context.Context, client *http.Client, url string, limit int64) (clientroute.Tags, error) {
	var tags clientroute.Tags
	release := ""
	note := func(p string) {
		if m := releasePath.FindStringSubmatch(p); m != nil && release == "" {
			release = m[1]
		}
	}
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		note(req.URL.Path)
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return tags, err
	}
	note(req.URL.Path)
	resp, err := c.Do(req)
	if err != nil {
		return tags, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return tags, fmt.Errorf("geo tags: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return tags, err
	}
	if int64(len(body)) > limit {
		return tags, fmt.Errorf("geo tags: archive is larger than %d bytes", limit)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return tags, err
	}
	tags.Release = release
	tags.Geosite, tags.Geoip = []string{}, []string{}
	for _, file := range zr.File {
		name := path.Base(file.Name)
		if !strings.HasSuffix(name, ".srs") || strings.HasSuffix(file.Name, "/") {
			continue
		}
		name = strings.TrimSuffix(name, ".srs")
		if code, ok := strings.CutPrefix(name, "geosite-"); ok && code != "" {
			tags.Geosite = append(tags.Geosite, code)
		} else if code, ok := strings.CutPrefix(name, "geoip-"); ok && code != "" {
			tags.Geoip = append(tags.Geoip, code)
		}
	}
	if len(tags.Geosite) == 0 || len(tags.Geoip) == 0 {
		return tags, fmt.Errorf("geo tags: archive has %d geosite and %d geoip rule sets", len(tags.Geosite), len(tags.Geoip))
	}
	return tags, nil
}

func (c *CoreClient) geoTags(ctx context.Context, client *http.Client, url string) ([]slog.Attr, error) {
	tags, err := fetchGeoTags(ctx, client, url, maxGeoTagsZip)
	if err != nil {
		return nil, err
	}
	var out struct {
		Changed bool `json:"changed"`
	}
	if err := c.api.Do(ctx, http.MethodPost, "/v1/maintenance/geo-tags", tags, &out); err != nil {
		return nil, err
	}
	return []slog.Attr{slog.String("release", tags.Release), slog.Int64("geosite", int64(len(tags.Geosite))),
		slog.Int64("geoip", int64(len(tags.Geoip))), slog.Bool("changed", out.Changed)}, nil
}
