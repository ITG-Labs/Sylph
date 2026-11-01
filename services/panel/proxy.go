package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

const (
	maxBodyBytes     = 64 << 10
	maxReplyBytes    = 8 << 20
	operatorHeader   = "X-Sylph-Operator"
	coreProxyPrefix  = "/api/core"
	maxNumericDigits = 19
	statusClientGone = 499
)

type coreRoute struct {
	method  string
	pattern string
	parts   []string
}

var coreAllowlist = buildAllowlist(
	"POST /v1/admin/groups",
	"GET /v1/admin/groups",
	"PATCH /v1/admin/groups/{id}",
	"DELETE /v1/admin/groups/{id}",
	"PUT /v1/admin/groups/{id}/members",
	"POST /v1/admin/plans",
	"GET /v1/admin/plans",
	"PATCH /v1/admin/plans/{id}",
	"POST /v1/admin/plans/{id}/invites",
	"GET /v1/admin/plans/{id}/invites",
	"DELETE /v1/admin/invites/{id}",
	"GET /v1/admin/plans/{id}/access",
	"PUT /v1/admin/plans/{id}/access/{telegram_id}",
	"DELETE /v1/admin/plans/{id}/access/{telegram_id}",
	"POST /v1/admin/nodes",
	"GET /v1/admin/nodes",
	"PATCH /v1/admin/nodes/{id}",
	"GET /v1/admin/nodes/{id}/stats",
	"GET /v1/admin/nodes/{id}/inbounds",
	"GET /v1/admin/inbounds",
	"GET /v1/admin/nodes/{id}/online",
	"POST /v1/admin/nodes/{id}/inbounds",
	"PATCH /v1/admin/inbounds/{id}",
	"DELETE /v1/admin/inbounds/{id}",
	"GET /v1/admin/addresses",
	"PUT /v1/admin/addresses/{id}/assignment",
	"DELETE /v1/admin/addresses/{id}/assignment",
	"DELETE /v1/admin/addresses/{id}",
	"GET /v1/admin/users",
	"GET /v1/admin/users/{id}",
	"GET /v1/admin/users/{id}/ips",
	"POST /v1/admin/users/{id}/ban",
	"POST /v1/admin/users/{id}/unban",
	"POST /v1/admin/users/{id}/extend",
	"POST /v1/admin/users/{id}/traffic",
	"POST /v1/admin/users/{id}/plan",
	"POST /v1/admin/periods/{id}/close",
	"GET /v1/admin/payments",
	"GET /v1/admin/payments/{payment_id}",
	"POST /v1/admin/payments/{payment_id}/recheck",
	"POST /v1/admin/payments/{payment_id}/apply",
	"POST /v1/admin/payments/{payment_id}/reject",
	"POST /v1/admin/payments/{payment_id}/refund",
	"GET /v1/admin/overview",
	"GET /v1/admin/audit",
	"DELETE /v1/admin/services/{cn}",
	"GET /v1/admin/settings",
	"PATCH /v1/admin/settings",
	"POST /v1/admin/settings/terms/bump",
	"GET /v1/admin/profiles",
	"POST /v1/admin/profiles",
	"GET /v1/admin/profiles/{id}",
	"PUT /v1/admin/profiles/{id}",
	"DELETE /v1/admin/profiles/{id}",
	"GET /v1/admin/profiles/{id}/preview",
	"GET /v1/admin/geo-sources",
	"POST /v1/admin/geo-sources",
	"PATCH /v1/admin/geo-sources/{id}",
	"DELETE /v1/admin/geo-sources/{id}",
	"POST /v1/admin/geo-sources/refresh",
	"GET /v1/users/{user_id}/stats",
	"POST /v1/users/{user_id}/rotate",
	"POST /v1/users/{user_id}/devices/reset",
	"DELETE /v1/users/{user_id}/devices/{device_id}",
	"POST /v1/payments",
)

func buildAllowlist(routes ...string) []coreRoute {
	out := make([]coreRoute, 0, len(routes))
	for _, r := range routes {
		method, pattern, _ := strings.Cut(r, " ")
		out = append(out, coreRoute{method: method, pattern: pattern, parts: strings.Split(strings.TrimPrefix(pattern, "/"), "/")})
	}
	return out
}

func matchRoute(method, escapedPath string) (coreRoute, string, bool) {
	if !strings.HasPrefix(escapedPath, "/") || strings.ContainsAny(escapedPath, "%\\") {
		return coreRoute{}, "", false
	}
	segs := strings.Split(escapedPath[1:], "/")
	for _, r := range coreAllowlist {
		if r.method != method || len(r.parts) != len(segs) {
			continue
		}
		ok := true
		for i, p := range r.parts {
			if !segmentMatches(p, segs[i]) {
				ok = false
				break
			}
		}
		if ok {
			return r, "/" + strings.Join(segs, "/"), true
		}
	}
	return coreRoute{}, "", false
}

func segmentMatches(part, seg string) bool {
	if !strings.HasPrefix(part, "{") {
		return part == seg
	}
	if part == "{payment_id}" {
		return validPaymentID(seg)
	}
	if seg == "" || len(seg) > maxNumericDigits {
		return false
	}
	for _, c := range seg {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func validPaymentID(id string) bool {
	if id == "" || len(id) > 64 || strings.Trim(id, ".") == "" {
		return false
	}
	for _, c := range id {
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

type CoreProxy struct {
	base   string
	client *http.Client
	log    *slog.Logger
}

func NewCoreProxy(base string, client *http.Client, logger *slog.Logger) (*CoreProxy, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("core url %q must be http(s)://host[:port]", base)
	}
	return &CoreProxy{base: strings.TrimRight(base, "/"), client: client, log: logger}, nil
}

func (p *CoreProxy) serve(w http.ResponseWriter, r *http.Request, operator string) {
	route, path, ok := matchRoute(r.Method, strings.TrimPrefix(r.URL.EscapedPath(), coreProxyPrefix))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such panel route")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		p.log.Warn("panel_proxy_body_failed", "admin", operator, "route", route.method+" "+route.pattern,
			"request_id", logging.RequestIDFromContext(r.Context()), "error", err.Error())
		writeError(w, http.StatusBadRequest, "bad_request", "cannot read body")
		return
	}
	if len(body) > maxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "body exceeds 64 KiB")
		return
	}
	target := p.base + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var rd io.Reader
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, rd)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		logging.RecordError(w, fmt.Errorf("build core request for %s %s: %w", route.method, route.pattern, err))
		writeError(w, http.StatusInternalServerError, "internal", "cannot build the core request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(operatorHeader, operator)
	req.Header.Set(logging.HeaderRequestID, logging.RequestIDFromContext(r.Context()))
	start := time.Now()
	resp, err := p.client.Do(req)
	status := 0
	defer func() {
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelWarn
		}
		p.log.Log(r.Context(), level, "panel_proxy", "admin", operator, "method", r.Method, "route", route.method+" "+route.pattern,
			"status", status, "duration_ms", time.Since(start).Milliseconds(), "request_id", logging.RequestIDFromContext(r.Context()))
	}()
	if err != nil {
		if r.Context().Err() != nil {
			status = statusClientGone
			w.WriteHeader(statusClientGone)
			return
		}
		status = http.StatusBadGateway
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		logging.RecordError(w, fmt.Errorf("core unavailable: %w", err))
		writeError(w, http.StatusBadGateway, "core_unavailable", "core is unreachable")
		return
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes+1))
	if err != nil || len(reply) > maxReplyBytes {
		if err != nil && r.Context().Err() != nil {
			status = statusClientGone
			w.WriteHeader(statusClientGone)
			return
		}
		status = http.StatusBadGateway
		if err == nil {
			err = fmt.Errorf("reply exceeds %d bytes", maxReplyBytes)
		}
		logging.RecordError(w, fmt.Errorf("read core reply: %w", err))
		writeError(w, http.StatusBadGateway, "core_unavailable", "core reply was cut")
		return
	}
	if rejected(resp.StatusCode, reply) {
		status = http.StatusBadGateway
		logging.RecordError(w, fmt.Errorf("core rejected the panel certificate (%d %q): check that it is not revoked or expired and has the panel role",
			resp.StatusCode, truncateRunes(strings.TrimSpace(string(reply)), 256)))
		writeError(w, http.StatusBadGateway, "core_rejected", "core rejected the panel certificate")
		return
	}
	status = resp.StatusCode
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(reply); err != nil {
		logging.RecordError(w, fmt.Errorf("write response: %w", err))
	}
}

func rejected(status int, reply []byte) bool {
	switch status {
	case http.StatusUnauthorized:
		return true
	case http.StatusForbidden:
		var e struct {
			Code string `json:"code"`
		}
		return json.Unmarshal(reply, &e) == nil && e.Code == "forbidden"
	}
	return false
}
