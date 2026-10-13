package panel

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

const (
	sessionCookie  = "sylph_session"
	csrfHeader     = "X-Sylph-Panel"
	maxLoginBytes  = 4 << 10
	contentPolicy  = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	assetsCache    = "public, max-age=31536000, immutable"
	unbuiltUIPage  = "Интерфейс панели не собран: npm run build в services/panel/web\n"
	maxLoggedRunes = 64
)

type Server struct {
	auth  *Auth
	core  *CoreProxy
	trust proxyTrust
	ui    fs.FS
	ttl   time.Duration
	log   *slog.Logger
}

func NewServer(auth *Auth, core *CoreProxy, trust proxyTrust, ui fs.FS, sessionTTL time.Duration, logger *slog.Logger) *Server {
	return &Server{auth: auth, core: core, trust: trust, ui: ui, ttl: sessionTTL, log: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("GET /auth/me", s.handleMe)
	mux.HandleFunc("/auth/", notFound)
	mux.HandleFunc("/api/", notFound)
	mux.HandleFunc("/", s.handleUI)
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, coreProxyPrefix+"/") {
			s.handleCore(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return logging.Middleware(s.log)(securityHeaders(csrfGuard(root)))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guarded := strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/")
		safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
		if guarded && !safe && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "csrf", "cross-site request refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	if r.Header.Get(csrfHeader) != "1" {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	return true
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLoginBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "expected {name, password, code}")
		return
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "unexpected data after the object")
		return
	}
	ip := s.trust.clientIP(r)
	id, err := s.auth.Login(body.Name, body.Password, body.Code, ip)
	logName := truncateRunes(body.Name, maxLoggedRunes)
	switch {
	case errors.Is(err, errAccountLocked):
		s.log.Error("panel_login", "name", logName, "ip", ip, "result", "account_locked")
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "too many failed logins, try later")
		return
	case errors.Is(err, errCodeAttemptsExhausted):
		s.log.Error("panel_login", "name", logName, "ip", ip, "result", "account_locked")
		writeError(w, http.StatusUnauthorized, "bad_credentials", "wrong name, password or code")
		return
	case errors.Is(err, errTooManyAttempts):
		s.log.Warn("panel_login", "name", logName, "ip", ip, "result", "blocked")
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "too many failed logins, try later")
		return
	case errors.Is(err, errBadCredentials):
		s.log.Warn("panel_login", "name", logName, "ip", ip, "result", "bad_credentials")
		writeError(w, http.StatusUnauthorized, "bad_credentials", "wrong name, password or code")
		return
	case err != nil:
		s.log.Error("panel_login", "name", logName, "ip", ip, "result", "error", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "login failed")
		return
	}
	s.log.Info("panel_login", "name", logName, "ip", ip, "result", "ok")
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: id, Path: "/", MaxAge: int(s.ttl / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"name": body.Name})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if name, ok := s.auth.Session(c.Value); ok {
			s.log.Info("panel_logout", "name", name)
		}
		s.auth.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	name, ok := s.sessionAdmin(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "log in first")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (s *Server) handleCore(w http.ResponseWriter, r *http.Request) {
	name, ok := s.sessionAdmin(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "log in first")
		return
	}
	s.core.serve(w, r, name)
}

func (s *Server) sessionAdmin(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	return s.auth.Session(c.Value)
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.ui == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, unbuiltUIPage)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name != "" && name != "index.html" && fs.ValidPath(name) {
		if st, err := fs.Stat(s.ui, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", assetsCache)
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, s.ui, name)
			return
		}
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
	}
	index, err := fs.ReadFile(s.ui, "index.html")
	if err != nil {
		http.Error(w, "index.html missing", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(index)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "no such route")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
