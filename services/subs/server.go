package subs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/logging"
)

const (
	coreTimeout  = 10 * time.Second
	maxHWIDRunes = 256
)

var tokenPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

var contentTypes = map[Format]string{
	FormatBase64:  "text/plain; charset=utf-8",
	FormatSingBox: "application/json; charset=utf-8",
	FormatClash:   "text/yaml; charset=utf-8",
}

type Settings struct {
	UpdateHours int
	Location    *time.Location
}

const defaultTitle = "Sylph"

func brandTitle(v *View) string {
	if strings.TrimSpace(v.Title) == "" {
		return defaultTitle
	}
	return v.Title
}

func supportURL(v *View) string {
	if strings.IndexFunc(v.SupportURL, func(r rune) bool { return r > unicode.MaxASCII || unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return ""
	}
	u, err := url.Parse(v.SupportURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	return v.SupportURL
}

type Server struct {
	core     Core
	settings Settings
	loc      *time.Location
	log      *slog.Logger
	newUUID  func() string
}

func NewServer(core Core, settings Settings, log *slog.Logger) *Server {
	loc := settings.Location
	if loc == nil {
		loc = time.FixedZone("MSK", 3*60*60)
	}
	return &Server{core: core, settings: settings, loc: loc, log: log, newUUID: logging.NewRequestID}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": version.Version}); err != nil {
			s.log.Warn("health_write_failed", "error", err.Error())
		}
	})
	mux.HandleFunc("GET /{token}", s.handleSubscription)
	mux.HandleFunc("GET /{token}/{$}", s.handleSubscription)
	return mux
}

type outcome struct {
	format Format
	status string
	code   int
	err    error
}

func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := logging.NewRequestID()
	ctx := logging.WithRequestID(r.Context(), id)
	w.Header().Set(logging.HeaderRequestID, id)
	res := s.serve(ctx, w, r)
	attrs := []slog.Attr{
		slog.String("request_id", id),
		slog.String("format", string(res.format)),
		slog.String("client", clientFamily(r.UserAgent())),
		slog.String("status", res.status),
		slog.Int("http_status", res.code),
		slog.Duration("duration", time.Since(start)),
	}
	level := slog.LevelInfo
	if res.err != nil {
		level = slog.LevelError
		attrs = append(attrs, slog.String("error", res.err.Error()))
	}
	s.log.LogAttrs(ctx, level, "subscription_request", attrs...)
}

func (s *Server) serve(ctx context.Context, w http.ResponseWriter, r *http.Request) outcome {
	format, formatErr := DetectFormat(r.URL.Query().Get("format"), r.UserAgent())
	res := outcome{format: format}
	fail := func(code int, status string) outcome {
		http.Error(w, http.StatusText(code), code)
		res.code, res.status = code, status
		return res
	}
	if !tokenPattern.MatchString(r.PathValue("token")) {
		return fail(http.StatusNotFound, "bad_token")
	}
	if wantsPage(r) {
		return s.servePage(ctx, w, r, r.PathValue("token"))
	}
	if formatErr != nil {
		return fail(http.StatusBadRequest, "bad_format")
	}
	hwid := r.Header.Get("X-Hwid")
	if utf8.RuneCountInString(hwid) > maxHWIDRunes {
		return fail(http.StatusBadRequest, "bad_hwid")
	}
	ctx, cancel := context.WithTimeout(ctx, coreTimeout)
	defer cancel()
	view, err := s.core.Subscription(ctx, Request{
		SubToken:    r.PathValue("token"),
		HWID:        hwid,
		UserAgent:   r.UserAgent(),
		DeviceOS:    strings.TrimSpace(r.Header.Get("X-Device-Os") + " " + r.Header.Get("X-Ver-Os")),
		DeviceModel: r.Header.Get("X-Device-Model"),
	})
	if errors.Is(err, ErrNotFound) {
		return fail(http.StatusNotFound, "not_found")
	}
	if err != nil {
		res = fail(http.StatusServiceUnavailable, "core_unavailable")
		res.err = err
		return res
	}
	res.status = view.Status
	conns, stub := content(view, format)
	if stub != "" {
		conns = []Connection{stubConnection(stub, s.newUUID())}
	}
	body, err := render(format, conns)
	if err != nil {
		res = fail(http.StatusInternalServerError, "render_failed")
		res.err = err
		return res
	}
	h := w.Header()
	h.Set("Content-Type", contentTypes[format])
	h.Set("Cache-Control", "no-store")
	h.Set("Subscription-Userinfo", userinfo(view))
	h.Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(brandTitle(view))))
	h.Set("Profile-Update-Interval", strconv.Itoa(s.settings.UpdateHours))
	if support := supportURL(view); support != "" {
		h.Set("Support-Url", support)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	res.code = http.StatusOK
	return res
}

func render(format Format, conns []Connection) ([]byte, error) {
	switch format {
	case FormatClash:
		return RenderClash(conns)
	case FormatSingBox:
		return RenderSingBox(conns)
	default:
		return RenderBase64(conns), nil
	}
}

func userinfo(view *View) string {
	var total, expire int64
	if view.TrafficLimitBytes != nil {
		total = *view.TrafficLimitBytes
	}
	if view.ExpiresAt != nil {
		expire = view.ExpiresAt.Unix()
	}
	return fmt.Sprintf("upload=0; download=%d; total=%d; expire=%d", view.TrafficUsedBytes, total, expire)
}

func clientFamily(ua string) string {
	family := ua
	if i := strings.IndexAny(family, "/ ;("); i >= 0 {
		family = family[:i]
	}
	if len(family) > 32 {
		family = family[:32]
	}
	if family == "" {
		return "unknown"
	}
	return family
}
